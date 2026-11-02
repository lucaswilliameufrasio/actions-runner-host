package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

const (
	defaultRunnerUsername = "github-runner"
	markerFileName        = ".host-prepared"
)

var (
	hostAliasPattern      = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	runnerUsernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	inspectionLinePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*=[A-Za-z0-9_.,:-]*$`)
)

var basePackages = []string{
	"ca-certificates",
	"curl",
	"tar",
	"git",
	"libkrb5-3",
	"zlib1g",
}

func reservedRunnerUsernames() []string {
	return []string{
		"admin",
		"backup",
		"bin",
		"daemon",
		"docker",
		"fleet",
		"fleet-agent",
		"games",
		"gnats",
		"irc",
		"list",
		"lp",
		"mail",
		"man",
		"messagebus",
		"news",
		"nobody",
		"proxy",
		"root",
		"sshd",
		"sudo",
		"sync",
		"sys",
		"systemd-network",
		"systemd-resolve",
		"systemd-timesync",
		"uucp",
		"www-data",
	}
}

func validateHostAlias(alias string) error {
	if len(alias) == 0 || len(alias) > 128 || !hostAliasPattern.MatchString(alias) {
		return errors.New("informe um alias SSH sem espaços (não há host padrão)")
	}
	return nil
}

func validateRunnerUsername(username string) error {
	if !runnerUsernamePattern.MatchString(username) {
		return errors.New("informe um nome de usuário Linux em minúsculas, sem espaços")
	}
	if isReservedRunnerUsername(username) {
		return errors.New("esse nome é reservado do sistema")
	}
	return nil
}

func isReservedRunnerUsername(username string) bool {
	for _, reserved := range reservedRunnerUsernames() {
		if username == reserved {
			return true
		}
	}
	return false
}

func runnerHome(username string) (string, error) {
	if err := validateRunnerUsername(username); err != nil {
		return "", err
	}
	return "/var/lib/" + username, nil
}

func runnerDirectory(username string) (string, error) {
	home, err := runnerHome(username)
	if err != nil {
		return "", err
	}
	return home + "/actions-runner", nil
}

type hostInspection struct {
	operatingSystemID string
	versionID         string
	architecture      string
	systemd           bool
	debianFamily      bool
	privilege         string
	suAvailable       bool
	tailscaleIPv4     string
	tailscaleInvalid  bool
	githubReachable   string
	userState         string
	userDetails       []string
}

type readiness struct {
	blockedReason string
	warnings      []string
}

func parseInspection(output string) (hostInspection, error) {
	fields, err := parseRemoteFields(output, []string{
		"os_id",
		"version_id",
		"arch",
		"systemd",
		"supported",
		"privilege",
		"su",
		"tailscale_ipv4",
		"github_reachable",
		"user_state",
		"user_detail",
	})
	if err != nil {
		return hostInspection{}, err
	}
	inspection := hostInspection{
		operatingSystemID: fields["os_id"],
		versionID:         fields["version_id"],
		architecture:      fields["arch"],
		privilege:         fields["privilege"],
		githubReachable:   fields["github_reachable"],
		userState:         fields["user_state"],
	}
	systemd, err := parseYesNo(fields["systemd"])
	if err != nil {
		return hostInspection{}, fmt.Errorf("campo systemd: %w", err)
	}
	inspection.systemd = systemd
	debianFamily, err := parseYesNo(fields["supported"])
	if err != nil {
		return hostInspection{}, fmt.Errorf("campo supported: %w", err)
	}
	inspection.debianFamily = debianFamily
	switch inspection.privilege {
	case "root", "sudo", "none":
	default:
		return hostInspection{}, errors.New("privilégio remoto inválido")
	}
	suAvailable, err := parseYesNo(fields["su"])
	if err != nil {
		return hostInspection{}, fmt.Errorf("campo su: %w", err)
	}
	inspection.suAvailable = suAvailable
	switch inspection.githubReachable {
	case "yes", "no", "unchecked":
	default:
		return hostInspection{}, errors.New("resultado de conectividade inválido")
	}
	switch inspection.userState {
	case "absent", "ready", "conflict":
	default:
		return hostInspection{}, errors.New("estado do usuário inválido")
	}
	details, err := parseUserDetails(fields["user_detail"], inspection.userState)
	if err != nil {
		return hostInspection{}, err
	}
	inspection.userDetails = details
	if fields["tailscale_ipv4"] != "" {
		parsedAddress := net.ParseIP(fields["tailscale_ipv4"])
		if parsedAddress == nil || parsedAddress.To4() == nil {
			inspection.tailscaleInvalid = true
		} else {
			inspection.tailscaleIPv4 = parsedAddress.String()
		}
	}
	return inspection, nil
}

func parseUserDetails(value string, userState string) ([]string, error) {
	if value == "" {
		if userState == "conflict" {
			return nil, errors.New("conflito de usuário sem motivo")
		}
		return nil, nil
	}
	if userState != "conflict" {
		return nil, errors.New("motivo de usuário inesperado")
	}
	parts := strings.Split(value, ",")
	details := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "home", "shell", "group", "root":
			details = append(details, part)
		default:
			return nil, errors.New("motivo de usuário inválido")
		}
	}
	return details, nil
}

func parseYesNo(value string) (bool, error) {
	switch value {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, errors.New("valor booleano inválido")
	}
}

func parseRemoteFields(output string, required []string) (map[string]string, error) {
	fields := make(map[string]string, len(required))
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, errors.New("inspeção remota vazia")
	}
	for _, line := range strings.Split(trimmed, "\n") {
		if !inspectionLinePattern.MatchString(line) {
			return nil, errors.New("linha inesperada na resposta remota")
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, errors.New("linha inesperada na resposta remota")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("campo remoto duplicado")
		}
		fields[key] = value
	}
	if len(fields) != len(required) {
		return nil, errors.New("resposta remota incompleta")
	}
	for _, key := range required {
		if _, exists := fields[key]; !exists {
			return nil, errors.New("resposta remota incompleta")
		}
	}
	return fields, nil
}

func assess(inspection hostInspection) readiness {
	warnings := make([]string, 0, 3)
	if !inspection.debianFamily {
		return readiness{blockedReason: "A VPS não é Debian ou Ubuntu. Nada foi alterado."}
	}
	if !inspection.systemd {
		return readiness{blockedReason: "A VPS não está usando systemd. Nada foi alterado."}
	}
	if inspection.privilege == "none" {
		if !inspection.suAvailable {
			return readiness{blockedReason: "O SSH não é root, não tem sudo sem senha e não há su. Nada foi alterado."}
		}
		warnings = append(warnings, "A conta SSH vira root com su -. A senha será pedida uma vez neste terminal e não será gravada.")
	}
	if inspection.userState == "conflict" {
		return readiness{blockedReason: conflictMessage(inspection)}
	}
	if inspection.tailscaleInvalid {
		warnings = append(warnings, "O Tailscale não retornou um IPv4 utilizável. O acesso segue pelo alias SSH.")
	} else if inspection.tailscaleIPv4 == "" {
		warnings = append(warnings, "Tailscale não retornou um IPv4. O acesso segue pelo alias SSH.")
	}
	switch inspection.githubReachable {
	case "no":
		warnings = append(warnings, "A VPS não alcançou https://api.github.com/zen.")
	case "unchecked":
		warnings = append(warnings, "curl ainda não está instalado; a saída para o GitHub será testada de novo após o preparo.")
	}
	return readiness{warnings: warnings}
}

func conflictMessage(inspection hostInspection) string {
	reasons := make([]string, 0, len(inspection.userDetails))
	for _, detail := range inspection.userDetails {
		switch detail {
		case "home":
			reasons = append(reasons, "o home não é o diretório reservado desta CLI")
		case "shell":
			reasons = append(reasons, "o shell não é nologin")
		case "group":
			reasons = append(reasons, "o usuário está em sudo, admin, wheel ou docker")
		case "root":
			reasons = append(reasons, "o uid é root")
		}
	}
	if len(reasons) == 0 {
		return "O usuário remoto já existe e não está no contrato desta CLI. Nada foi alterado."
	}
	return "O usuário remoto já existe e não será modificado (" + strings.Join(reasons, "; ") + "). Nada foi alterado."
}

func confirmSummary(hostAlias string, username string, inspection hostInspection, warnings []string) (string, error) {
	home, err := runnerHome(username)
	if err != nil {
		return "", err
	}
	directory, err := runnerDirectory(username)
	if err != nil {
		return "", err
	}
	userDescription := "ausente, será criado sem sudo"
	if inspection.userState == "ready" {
		userDescription = "já conforme, será mantido sem sudo"
	}
	privilege := "sudo sem senha"
	switch inspection.privilege {
	case "root":
		privilege = "root pelo SSH"
	case "none":
		privilege = "su - depois do login SSH"
	}
	tailscale := "ausente"
	if inspection.tailscaleIPv4 != "" {
		tailscale = inspection.tailscaleIPv4
	}
	github := "ainda não testada"
	switch inspection.githubReachable {
	case "yes":
		github = "alcançou api.github.com"
	case "no":
		github = "não alcançou api.github.com"
	}
	archiveArchitecture := "não mapeada (" + inspection.architecture + ")"
	mappedArchitecture, recognized := actionsRunnerArchitecture(inspection.architecture)
	if recognized {
		archiveArchitecture = mappedArchitecture
	}
	var builder strings.Builder
	builder.WriteString("Preparo da VPS. O agent do GitHub Actions não será configurado.\n\n")
	fmt.Fprintf(&builder, "Host SSH: %s\n", hostAlias)
	fmt.Fprintf(&builder, "Usuário: %s\n", username)
	fmt.Fprintf(&builder, "Home: %s\n", home)
	fmt.Fprintf(&builder, "Diretório do runner: %s\n", directory)
	fmt.Fprintf(&builder, "Sistema: %s %s (%s)\n", inspection.operatingSystemID, inspection.versionID, inspection.architecture)
	fmt.Fprintf(&builder, "Arquitetura do pacote actions/runner: %s\n", archiveArchitecture)
	fmt.Fprintf(&builder, "Privilégio remoto: %s\n", privilege)
	fmt.Fprintf(&builder, "Tailscale IPv4: %s\n", tailscale)
	fmt.Fprintf(&builder, "Saída para o GitHub: %s\n", github)
	fmt.Fprintf(&builder, "Usuário remoto: %s\n\n", userDescription)
	builder.WriteString("A CLI vai criar ou reaproveitar o usuário, ajustar os diretórios e instalar pacotes apt: ")
	builder.WriteString(strings.Join(basePackages, ", "))
	builder.WriteString(", mais libssl, liblttng e libicu disponíveis na distro.\n")
	builder.WriteString("Não registra o runner, não guarda token, não altera firewall e não coloca o usuário no grupo docker.\n")
	if len(warnings) > 0 {
		builder.WriteString("\nAvisos:\n")
		for _, warning := range warnings {
			builder.WriteString("- " + warning + "\n")
		}
	}
	return builder.String(), nil
}

func actionsRunnerArchitecture(machine string) (string, bool) {
	switch machine {
	case "x86_64":
		return "x64", true
	case "aarch64", "arm64":
		return "arm64", true
	default:
		return "", false
	}
}

func nextSteps(username string, inspection hostInspection) (string, error) {
	home, err := runnerHome(username)
	if err != nil {
		return "", err
	}
	directory, err := runnerDirectory(username)
	if err != nil {
		return "", err
	}
	archiveArchitecture, recognized := actionsRunnerArchitecture(inspection.architecture)
	archiveHint := "Baixe o pacote Linux correspondente a " + inspection.architecture + " na página de releases."
	if recognized {
		archiveHint = "Baixe actions-runner-linux-" + archiveArchitecture + "-<versão>.tar.gz na página de releases."
	}
	var builder strings.Builder
	builder.WriteString("\nHost preparado. O agent ainda não foi configurado e nenhum token foi usado.\n\n")
	builder.WriteString("Próximo passo, como root, sem conceder sudo ao usuário do runner:\n\n")
	builder.WriteString("1. " + archiveHint + "\n")
	builder.WriteString("   Confira o SHA256 publicado em https://github.com/actions/runner/releases\n")
	fmt.Fprintf(&builder, "2. Extraia o conteúdo em %s e dê o diretório a %s.\n", directory, username)
	fmt.Fprintf(&builder, "3. Como o usuário, rode ./config.sh apenas num repositório privado:\n   runuser -u %s -- ./config.sh --url https://github.com/<dono>/<repo-privado> --token <token-de-registro>\n", username)
	fmt.Fprintf(&builder, "4. Ainda em %s, como root: ./svc.sh install %s && ./svc.sh start\n", directory, username)
	builder.WriteString("5. Confira no unit gerado que User=" + username + " e que este usuário continua sem sudo e fora do grupo docker.\n\n")
	fmt.Fprintf(&builder, "Home reservado: %s (modo 0700).\n", home)
	builder.WriteString("Não use este runner em repositório público nem em workflow de fork.\n")
	return builder.String(), nil
}

type hostVerification struct {
	homeMode        string
	homeOwner       string
	runnerOwner     string
	marker          bool
	sudoAllowed     bool
	privilegedGroup bool
	shellOK         bool
	githubReachable string
}

func parseVerification(output string) (hostVerification, error) {
	fields, err := parseRemoteFields(output, []string{
		"home_mode",
		"home_owner",
		"runner_owner",
		"marker",
		"sudo_allowed",
		"privileged_group",
		"shell_ok",
		"github_reachable",
	})
	if err != nil {
		return hostVerification{}, err
	}
	verification := hostVerification{
		homeMode:    fields["home_mode"],
		homeOwner:   fields["home_owner"],
		runnerOwner: fields["runner_owner"],
	}
	marker, err := parseYesNo(fields["marker"])
	if err != nil {
		return hostVerification{}, err
	}
	verification.marker = marker
	sudoAllowed, err := parseYesNo(fields["sudo_allowed"])
	if err != nil {
		return hostVerification{}, err
	}
	verification.sudoAllowed = sudoAllowed
	privilegedGroup, err := parseYesNo(fields["privileged_group"])
	if err != nil {
		return hostVerification{}, err
	}
	verification.privilegedGroup = privilegedGroup
	shellOK, err := parseYesNo(fields["shell_ok"])
	if err != nil {
		return hostVerification{}, err
	}
	verification.shellOK = shellOK
	switch fields["github_reachable"] {
	case "yes", "no", "unchecked":
		verification.githubReachable = fields["github_reachable"]
	default:
		return hostVerification{}, errors.New("resultado de conectividade inválido")
	}
	return verification, nil
}

func explainVerification(verification hostVerification, username string) error {
	problems := make([]string, 0, 5)
	if verification.homeMode != "700" || verification.homeOwner != username {
		problems = append(problems, "home")
	}
	if verification.runnerOwner != username {
		problems = append(problems, "diretório do runner")
	}
	if !verification.marker {
		problems = append(problems, "marcador de preparo")
	}
	if verification.sudoAllowed {
		problems = append(problems, "sudo")
	}
	if verification.privilegedGroup {
		problems = append(problems, "grupo privilegiado")
	}
	if !verification.shellOK {
		problems = append(problems, "shell")
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("verificação final falhou (%s); o agent não foi configurado", strings.Join(problems, ", "))
}
