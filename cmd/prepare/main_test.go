package main

import (
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHostAliasValidation(t *testing.T) {
	valid := []string{"production-arm", "vps-prod", "100.64.0.1", "my.host"}
	for _, alias := range valid {
		if err := validateHostAlias(alias); err != nil {
			t.Errorf("host alias %q should be accepted: %v", alias, err)
		}
	}
	invalid := []string{"", "two words", "user@host", "host;rm -rf /", strings.Repeat("a", 129)}
	for _, alias := range invalid {
		if err := validateHostAlias(alias); err == nil {
			t.Errorf("host alias %q should be rejected", alias)
		}
	}
}

func TestRunnerUsernameValidation(t *testing.T) {
	if err := validateRunnerUsername(defaultRunnerUsername); err != nil {
		t.Fatalf("default username should be accepted: %v", err)
	}
	invalid := []string{"", "Root", "root", "docker", "sudo", "two words", "../etc", "github-runner;id"}
	for _, username := range invalid {
		if err := validateRunnerUsername(username); err == nil {
			t.Errorf("username %q should be rejected", username)
		}
	}
}

func TestShellQuoteArgs(t *testing.T) {
	arguments := []string{"bash", "-s", "--", "it's-safe"}
	quoted := shellQuoteArgs(arguments)
	expected := []string{"'bash'", "'-s'", "'--'", `'it'\''s-safe'`}
	if len(quoted) != len(expected) {
		t.Fatalf("shellQuoteArgs()=%q, expected %q", quoted, expected)
	}
	for index, actual := range quoted {
		if actual != expected[index] {
			t.Fatalf("shellQuoteArgs()=%q, expected %q", quoted, expected)
		}
	}
}

func TestRemoteInterpreter(t *testing.T) {
	inspectionArguments, err := remoteInterpreter("none", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(inspectionArguments, " ") != "bash -s --" {
		t.Fatalf("inspection interpreter = %q", inspectionArguments)
	}
	sudoArguments, err := remoteInterpreter("sudo", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sudoArguments, " ") != "sudo -n bash -s --" {
		t.Fatalf("sudo interpreter = %q", sudoArguments)
	}
	if _, err := remoteInterpreter("none", true); err == nil {
		t.Fatal("missing privilege should be rejected before any remote change")
	}
}

func TestSSHCommandUsesValidatedAlias(t *testing.T) {
	command, err := sshScriptCommand("vps-app", "sudo", true, "true\n", defaultRunnerUsername)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	if !strings.Contains(joined, " vps-app ") || !strings.Contains(joined, "'sudo' '-n' 'bash' '-s' '--' '"+defaultRunnerUsername+"'") {
		t.Fatalf("ssh arguments = %q", command.Args)
	}
	if _, err := sshScriptCommand("bad host", "root", true, "true\n", defaultRunnerUsername); err == nil {
		t.Fatal("invalid host should be rejected before SSH")
	}
}

func TestInspectionParser(t *testing.T) {
	inspection, err := parseInspection(readyInspection("absent", ""))
	if err != nil {
		t.Fatal(err)
	}
	if inspection.architecture != "aarch64" || inspection.privilege != "sudo" || inspection.userState != "absent" {
		t.Fatalf("inspection = %+v", inspection)
	}
	if inspection.tailscaleIPv4 != "100.64.0.1" {
		t.Fatalf("tailscale = %q", inspection.tailscaleIPv4)
	}

	invalidAddress := strings.Replace(readyInspection("absent", ""), "tailscale_ipv4=100.64.0.1", "tailscale_ipv4=999.1.1.1", 1)
	inspection, err = parseInspection(invalidAddress)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.tailscaleInvalid || inspection.tailscaleIPv4 != "" {
		t.Fatal("invalid Tailscale address should become a warning, not a parsed address")
	}

	if _, err := parseInspection(readyInspection("conflict", "")); err == nil {
		t.Fatal("conflict without a reason should be rejected")
	}
	if _, err := parseInspection(readyInspection("absent", "home")); err == nil {
		t.Fatal("detail on a non-conflict user should be rejected")
	}
	if _, err := parseInspection(readyInspection("absent", "") + "token=secret\n"); err == nil {
		t.Fatal("unexpected remote field should be rejected")
	}
}

func TestAssessBlocksUnsafeHosts(t *testing.T) {
	inspection, err := parseInspection(readyInspection("conflict", "group,shell"))
	if err != nil {
		t.Fatal(err)
	}
	decision := assess(inspection)
	if decision.blockedReason == "" || !strings.Contains(decision.blockedReason, "docker") {
		t.Fatalf("blocked reason = %q", decision.blockedReason)
	}

	unsupported := strings.Replace(readyInspection("absent", ""), "supported=yes", "supported=no", 1)
	inspection, err = parseInspection(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	if assess(inspection).blockedReason == "" {
		t.Fatal("non-Debian host should be blocked")
	}
}

func TestSummaryDoesNotRegisterTheRunner(t *testing.T) {
	inspection, err := parseInspection(readyInspection("absent", ""))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := confirmSummary("vps-app", defaultRunnerUsername, inspection, assess(inspection).warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Não registra o runner") || !strings.Contains(summary, "não coloca o usuário no grupo docker") {
		t.Fatalf("summary missing the safety boundary:\n%s", summary)
	}
	if !strings.Contains(summary, "/var/lib/"+defaultRunnerUsername+"/actions-runner") {
		t.Fatalf("summary missing the runner directory:\n%s", summary)
	}
	steps, err := nextSteps(defaultRunnerUsername, inspection)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(steps, "O agent ainda não foi configurado") || !strings.Contains(steps, "actions-runner-linux-arm64-") {
		t.Fatalf("next steps = %s", steps)
	}
}

func TestAssessAllowsLoginSu(t *testing.T) {
	raw := strings.Replace(readyInspection("absent", ""), "privilege=sudo", "privilege=none", 1)
	inspection, err := parseInspection(raw)
	if err != nil {
		t.Fatal(err)
	}
	decision := assess(inspection)
	if decision.blockedReason != "" {
		t.Fatalf("su - should be accepted: %s", decision.blockedReason)
	}
	summary, err := confirmSummary("vps-app", defaultRunnerUsername, inspection, decision.warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "su - depois do login SSH") || !strings.Contains(summary, "não será gravada") {
		t.Fatalf("summary = %s", summary)
	}
	withoutSu := strings.Replace(raw, "su=yes", "su=no", 1)
	inspection, err = parseInspection(withoutSu)
	if err != nil {
		t.Fatal(err)
	}
	if assess(inspection).blockedReason == "" {
		t.Fatal("missing su should block before any change")
	}
}

func TestSuRemoteArguments(t *testing.T) {
	preparePath := "/home/admin/.local/state/actions-runner-host/prepare.Abcdef"
	verifyPath := "/home/admin/.local/state/actions-runner-host/verify.Abcdef"
	resultPath := "/home/admin/.local/state/actions-runner-host/result.Abcdef"
	arguments, err := suRemoteArguments(preparePath, verifyPath, resultPath, defaultRunnerUsername)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(arguments[:3], " ") != "su - -c" {
		t.Fatalf("su arguments = %q", arguments)
	}
	if !strings.Contains(arguments[3], "&& /bin/bash "+verifyPath) || strings.Contains(arguments[3], "sudo") {
		t.Fatalf("su command = %s", arguments[3])
	}
	command, err := sshCommand("vps-app", true, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	if !strings.Contains(joined, " -tt ") || !strings.Contains(joined, "'su' '-' '-c'") {
		t.Fatalf("ssh arguments = %q", command.Args)
	}
	if _, err := suRemoteArguments("/tmp/prepare.Abcdef", verifyPath, resultPath, defaultRunnerUsername); err == nil {
		t.Fatal("staging path outside the private directory should be rejected")
	}
	if _, err := parseStagedFiles("prepare=/tmp/../home/admin/.local/state/actions-runner-host/prepare.Abcdef\n"); err == nil {
		t.Fatal("path traversal should be rejected")
	}
}

func TestVerificationRejectsSudo(t *testing.T) {
	verification, err := parseVerification(strings.Join([]string{
		"home_mode=700",
		"home_owner=" + defaultRunnerUsername,
		"runner_owner=" + defaultRunnerUsername,
		"marker=yes",
		"sudo_allowed=yes",
		"privileged_group=no",
		"shell_ok=yes",
		"github_reachable=yes",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := explainVerification(verification, defaultRunnerUsername); err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("sudo verification error = %v", err)
	}
}

func TestApplyScriptStopsBeforeTheAgent(t *testing.T) {
	script, err := applyScript()
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		"useradd --system --create-home",
		"--shell",
		"apt-get update",
		"ca-certificates",
		"libicu",
		"sudo -n -l -U",
		"reserved username",
	}
	for _, fragment := range required {
		if !strings.Contains(script, fragment) {
			t.Errorf("apply script missing %q", fragment)
		}
	}
	forbidden := []string{
		"config.sh",
		"svc.sh",
		"NOPASSWD",
		"/etc/sudoers",
		"usermod -aG",
		"registration-token",
		"{{",
		defaultRunnerUsername,
	}
	for _, fragment := range forbidden {
		if strings.Contains(script, fragment) {
			t.Errorf("apply script contains %q", fragment)
		}
	}
	for _, name := range reservedRunnerUsernames() {
		if !strings.Contains(script, name) {
			t.Errorf("reserved username %s missing from the remote guard", name)
		}
	}
	assertBashSyntax(t, script)
	assertBashSyntax(t, stageFilesScript)
	inspect, err := inspectScript()
	if err != nil {
		t.Fatal(err)
	}
	verify, err := verifyScript()
	if err != nil {
		t.Fatal(err)
	}
	assertBashSyntax(t, inspect)
	assertBashSyntax(t, verify)
}

func TestPrompts(t *testing.T) {
	host := newHostPrompt()
	host.input.SetValue("bad host")
	updated, _ := host.Update(keyPress(tea.KeyEnter))
	host = updated.(hostPrompt)
	if host.done || host.problem == "" {
		t.Fatal("invalid host should stay on the prompt")
	}
	host.input.SetValue("vps-app")
	updated, _ = host.Update(keyPress(tea.KeyEnter))
	host = updated.(hostPrompt)
	if !host.done || host.host != "vps-app" {
		t.Fatalf("host prompt = %+v", host)
	}

	username := newUsernamePrompt()
	updated, _ = username.Update(keyPress(tea.KeyEnter))
	username = updated.(usernamePrompt)
	if !username.done || username.username != defaultRunnerUsername {
		t.Fatalf("empty username prompt = %+v", username)
	}

	confirmation := newConfirmPrompt("resumo")
	updated, _ = confirmation.Update(keyPress(tea.KeyDown))
	confirmation = updated.(confirmPrompt)
	updated, _ = confirmation.Update(keyPress(tea.KeyEnter))
	confirmation = updated.(confirmPrompt)
	if !confirmation.done || confirmation.accepted {
		t.Fatal("the second choice must cancel without applying")
	}
}

func readyInspection(userState string, userDetail string) string {
	return strings.Join([]string{
		"os_id=ubuntu",
		"version_id=24.04",
		"arch=aarch64",
		"systemd=yes",
		"supported=yes",
		"privilege=sudo",
		"su=yes",
		"tailscale_ipv4=100.64.0.1",
		"github_reachable=unchecked",
		"user_state=" + userState,
		"user_detail=" + userDetail,
	}, "\n")
}

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func assertBashSyntax(t *testing.T, script string) {
	t.Helper()
	command := exec.Command("bash", "-n")
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, output)
	}
}
