package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func remoteInterpreter(privilege string, requireRoot bool) ([]string, error) {
	if !requireRoot {
		return []string{"bash", "-s", "--"}, nil
	}
	switch privilege {
	case "root":
		return []string{"bash", "-s", "--"}, nil
	case "sudo":
		return []string{"sudo", "-n", "bash", "-s", "--"}, nil
	default:
		return nil, errors.New("remote administration requires root or passwordless sudo")
	}
}

func sshCommand(host string, allocateTerminal bool, remoteArguments ...string) (*exec.Cmd, error) {
	if err := validateHostAlias(host); err != nil {
		return nil, err
	}
	sshArguments := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if allocateTerminal {
		sshArguments = append(sshArguments, "-tt")
	}
	sshArguments = append(sshArguments, host)
	sshArguments = append(sshArguments, shellQuoteArgs(remoteArguments)...)
	// #nosec G204 -- The host alias is validated and remote arguments are shell-quoted.
	return exec.Command("ssh", sshArguments...), nil
}

func sshScriptCommand(host string, privilege string, requireRoot bool, script string, username string) (*exec.Cmd, error) {
	if err := validateRunnerUsername(username); err != nil {
		return nil, err
	}
	interpreter, err := remoteInterpreter(privilege, requireRoot)
	if err != nil {
		return nil, err
	}
	command, err := sshCommand(host, false, append(interpreter, username)...)
	if err != nil {
		return nil, err
	}
	command.Stdin = strings.NewReader(script)
	return command, nil
}

func runRemoteScript(host string, privilege string, requireRoot bool, script string, username string) error {
	command, err := sshScriptCommand(host, privilege, requireRoot, script, username)
	if err != nil {
		return err
	}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("ssh remoto: %w", err)
	}
	return nil
}

func remoteScriptOutput(host string, privilege string, requireRoot bool, script string, username string) (string, error) {
	command, err := sshScriptCommand(host, privilege, requireRoot, script, username)
	if err != nil {
		return "", err
	}
	var standardError bytes.Buffer
	command.Stderr = &standardError
	standardOutput, err := command.Output()
	if err != nil {
		detail := trimRemoteDetail(standardError.String())
		if detail == "" {
			return "", fmt.Errorf("ssh remoto: %w", err)
		}
		return "", fmt.Errorf("ssh remoto: %w: %s", err, detail)
	}
	return string(standardOutput), nil
}

func trimRemoteDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) > 500 {
		return detail[:500]
	}
	return detail
}

func shellQuoteArgs(arguments []string) []string {
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, "'"+strings.ReplaceAll(argument, "'", `'\''`)+"'")
	}
	return quoted
}

type stagedFiles struct {
	preparePath string
	verifyPath  string
	resultPath  string
}

func applyAndVerifyThroughSu(host string, prepareScript string, verifyScript string, username string) (string, error) {
	staged, err := stageRemoteFiles(host)
	if err != nil {
		return "", err
	}
	defer removeStagedFiles(host, staged)
	if err := uploadRemoteFile(host, staged.preparePath, prepareScript); err != nil {
		return "", err
	}
	if err := uploadRemoteFile(host, staged.verifyPath, verifyScript); err != nil {
		return "", err
	}
	arguments, err := suRemoteArguments(staged.preparePath, staged.verifyPath, staged.resultPath, username)
	if err != nil {
		return "", err
	}
	command, err := sshCommand(host, true, arguments...)
	if err != nil {
		return "", err
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	fmt.Fprintln(os.Stderr, "O preparo e a verificação usam um único su -. Digite a senha de root; ela não é gravada.")
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("su -: %w", err)
	}
	return readRemoteFile(host, staged.resultPath)
}

func stageRemoteFiles(host string) (stagedFiles, error) {
	command, err := sshCommand(host, false, "bash", "-s")
	if err != nil {
		return stagedFiles{}, err
	}
	command.Stdin = strings.NewReader(stageFilesScript)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	standardOutput, err := command.Output()
	if err != nil {
		return stagedFiles{}, fmt.Errorf("preparar arquivos do su: %w", err)
	}
	return parseStagedFiles(string(standardOutput))
}

func parseStagedFiles(output string) (stagedFiles, error) {
	staged := stagedFiles{}
	seen := map[string]string{}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return stagedFiles{}, errors.New("o su não recebeu caminhos temporários")
	}
	for _, line := range strings.Split(trimmed, "\n") {
		kind, path, found := strings.Cut(line, "=")
		if !found {
			return stagedFiles{}, errors.New("caminho temporário do su foi rejeitado")
		}
		if err := validateStagedPath(path, kind); err != nil {
			return stagedFiles{}, err
		}
		if _, exists := seen[kind]; exists {
			return stagedFiles{}, errors.New("caminho temporário do su foi rejeitado")
		}
		seen[kind] = path
	}
	staged.preparePath = seen["prepare"]
	staged.verifyPath = seen["verify"]
	staged.resultPath = seen["result"]
	if staged.preparePath == "" || staged.verifyPath == "" || staged.resultPath == "" {
		return stagedFiles{}, errors.New("caminho temporário do su foi rejeitado")
	}
	return staged, nil
}

func validateStagedPath(path string, kind string) error {
	marker := "/.local/state/actions-runner-host/" + kind + "."
	if kind != "prepare" && kind != "verify" && kind != "result" {
		return errors.New("caminho temporário do su foi rejeitado")
	}
	if strings.Contains(path, "..") || !strings.HasPrefix(path, "/") || !strings.Contains(path, marker) {
		return errors.New("caminho temporário do su foi rejeitado")
	}
	for _, character := range path {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '/' && character != '.' && character != '_' && character != '-' {
			return errors.New("caminho temporário do su foi rejeitado")
		}
	}
	return nil
}

func suRemoteArguments(preparePath string, verifyPath string, resultPath string, username string) ([]string, error) {
	if err := validateStagedPath(preparePath, "prepare"); err != nil {
		return nil, err
	}
	if err := validateStagedPath(verifyPath, "verify"); err != nil {
		return nil, err
	}
	if err := validateStagedPath(resultPath, "result"); err != nil {
		return nil, err
	}
	if err := validateRunnerUsername(username); err != nil {
		return nil, err
	}
	commandText := "/bin/bash " + preparePath + " " + username + " && /bin/bash " + verifyPath + " " + username + " > " + resultPath
	return []string{"su", "-", "-c", commandText}, nil
}

func uploadRemoteFile(host string, path string, contents string) error {
	if err := validateStagedPath(path, stagedKind(path)); err != nil {
		return err
	}
	command, err := sshCommand(host, false, "bash", "-c", `cat > "$1"`, "bash", path)
	if err != nil {
		return err
	}
	command.Stdin = strings.NewReader(contents)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("enviar script: %w", err)
	}
	return nil
}

func readRemoteFile(host string, path string) (string, error) {
	if err := validateStagedPath(path, "result"); err != nil {
		return "", err
	}
	command, err := sshCommand(host, false, "cat", "--", path)
	if err != nil {
		return "", err
	}
	var standardError bytes.Buffer
	command.Stderr = &standardError
	standardOutput, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("ler verificação: %w", err)
	}
	return string(standardOutput), nil
}

func removeStagedFiles(host string, staged stagedFiles) {
	paths := []string{staged.preparePath, staged.verifyPath, staged.resultPath}
	for _, path := range paths {
		if err := validateStagedPath(path, stagedKind(path)); err != nil {
			fmt.Fprintf(os.Stderr, "prepare: caminho temporário não removido: %v\n", err)
			return
		}
	}
	arguments := append([]string{"rm", "-f", "--"}, paths...)
	command, err := sshCommand(host, false, arguments...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare: não consegui remover os arquivos temporários do su: %v\n", err)
		return
	}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "prepare: não consegui remover os arquivos temporários do su: %v\n", err)
	}
}

func stagedKind(path string) string {
	switch {
	case strings.Contains(path, "/.local/state/actions-runner-host/prepare."):
		return "prepare"
	case strings.Contains(path, "/.local/state/actions-runner-host/verify."):
		return "verify"
	case strings.Contains(path, "/.local/state/actions-runner-host/result."):
		return "result"
	default:
		return ""
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
