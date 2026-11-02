package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "prepare:", err)
		os.Exit(1)
	}
}

func run() error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return errors.New("execute em um terminal interativo; não há host SSH padrão")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return errors.New("comando obrigatório não encontrado no PATH: ssh")
	}
	host, err := promptHost()
	if err != nil {
		return err
	}
	username, err := promptUsername()
	if err != nil {
		return err
	}
	fmt.Printf("Consultando %s sem alterar a VPS...\n", host)
	inspection, err := inspectHost(host, username)
	if err != nil {
		return err
	}
	decision := assess(inspection)
	if decision.blockedReason != "" {
		return errors.New(decision.blockedReason)
	}
	summary, err := confirmSummary(host, username, inspection, decision.warnings)
	if err != nil {
		return err
	}
	accepted, err := promptConfirm(summary)
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("preparo cancelado; a VPS não foi alterada")
	}
	fmt.Printf("Aplicando o preparo em %s. O agent não será registrado.\n", host)
	verification, err := executePreparation(host, inspection, username)
	if err != nil {
		return err
	}
	if err := explainVerification(verification, username); err != nil {
		return err
	}
	steps, err := nextSteps(username, inspection)
	if err != nil {
		return err
	}
	fmt.Print(steps)
	if verification.githubReachable != "yes" {
		fmt.Println("Aviso: a VPS ainda não confirmou saída HTTPS para api.github.com.")
	}
	return nil
}

func inspectHost(host string, username string) (hostInspection, error) {
	script, err := inspectScript()
	if err != nil {
		return hostInspection{}, err
	}
	output, err := remoteScriptOutput(host, "none", false, script, username)
	if err != nil {
		return hostInspection{}, fmt.Errorf("não consegui inspecionar o host: %w", err)
	}
	inspection, err := parseInspection(output)
	if err != nil {
		return hostInspection{}, fmt.Errorf("resposta de inspeção rejeitada: %w", err)
	}
	return inspection, nil
}

func executePreparation(host string, inspection hostInspection, username string) (hostVerification, error) {
	prepare, err := applyScript()
	if err != nil {
		return hostVerification{}, err
	}
	verify, err := verifyScript()
	if err != nil {
		return hostVerification{}, err
	}
	if inspection.privilege == "none" {
		output, err := applyAndVerifyThroughSu(host, prepare, verify, username)
		if err != nil {
			return hostVerification{}, fmt.Errorf("preparo remoto falhou; o agent não foi configurado e nenhum usuário foi removido: %w", err)
		}
		verification, err := parseVerification(output)
		if err != nil {
			return hostVerification{}, fmt.Errorf("não consegui verificar o preparo; o agent não foi configurado: %w", err)
		}
		return verification, nil
	}
	if err := runRemoteScript(host, inspection.privilege, true, prepare, username); err != nil {
		return hostVerification{}, fmt.Errorf("preparo remoto falhou; o agent não foi configurado e nenhum usuário foi removido: %w", err)
	}
	fmt.Println("Verificando o usuário, os diretórios e a ausência de sudo...")
	verification, err := verifyHost(host, inspection.privilege, username)
	if err != nil {
		return hostVerification{}, fmt.Errorf("não consegui verificar o preparo; o agent não foi configurado: %w", err)
	}
	return verification, nil
}

func verifyHost(host string, privilege string, username string) (hostVerification, error) {
	script, err := verifyScript()
	if err != nil {
		return hostVerification{}, err
	}
	output, err := remoteScriptOutput(host, privilege, true, script, username)
	if err != nil {
		return hostVerification{}, err
	}
	return parseVerification(output)
}
