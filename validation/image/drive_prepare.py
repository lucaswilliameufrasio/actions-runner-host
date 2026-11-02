#!/usr/bin/env python3
"""Drive the prepare TUI through SSH and one su password prompt."""

import fcntl
import os
import pty
import re
import select
import signal
import struct
import sys
import termios
import time

ANSI_PATTERN = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")
PASSWORD_PROMPT_PATTERN = re.compile(r"(?i)(password|senha)\s*:")


def strip_ansi(value: str) -> str:
    return ANSI_PATTERN.sub("", value)


def read_password(path: str) -> str:
    with open(path, "r", encoding="utf-8") as handle:
        password = handle.read().strip()
    if password == "" or any(character in password for character in "\n\r:"):
        raise SystemExit("root password file is unusable")
    return password


def set_window_size(file_descriptor: int) -> None:
    window = struct.pack("HHHH", 40, 120, 0, 0)
    fcntl.ioctl(file_descriptor, termios.TIOCSWINSZ, window)


class PrepareDriver:
    def __init__(self, password: str, transcript_path: str, expected_user: str) -> None:
        self.password = password
        self.transcript_path = transcript_path
        self.expected_user = expected_user
        self.visible = ""
        self.stage = "host"
        self.master = -1
        self.pending_text = ""
        self.pending_at = 0.0
        self.password_region_start = 0

    def append(self, chunk: bytes) -> None:
        text = chunk.decode("utf-8", errors="replace").replace(self.password, "[redacted]")
        self.visible += text
        with open(self.transcript_path, "a", encoding="utf-8") as handle:
            handle.write(text)

    def plain(self) -> str:
        return strip_ansi(self.visible)

    def queue(self, text: str, next_stage: str) -> None:
        if next_stage == "password":
            self.password_region_start = len(self.plain())
        self.pending_text = text
        self.pending_at = time.monotonic() + 0.3
        self.stage = next_stage
        print(f"prepare stage {next_stage}", file=sys.stderr)

    def flush_pending(self) -> None:
        if self.pending_text == "" or time.monotonic() < self.pending_at:
            return
        os.write(self.master, self.pending_text.encode("utf-8"))
        self.pending_text = ""

    def advance(self) -> None:
        if self.pending_text != "":
            return
        plain = self.plain()
        if "A VPS não está usando systemd" in plain:
            raise SystemExit("prepare blocked because systemd is not running")
        if "Authentication failure" in plain or "falha de autenticação" in plain.lower():
            raise SystemExit("su rejected the password")
        if self.stage == "host" and "Alias SSH" in plain:
            self.queue("127.0.0.1\r", "username")
            return
        if self.stage == "username" and "Usuário do runner" in plain:
            self.queue("\r", "confirm")
            return
        if self.stage == "confirm" and "Privilégio remoto: su - depois do login SSH" in plain and "> aplicar" in plain:
            self.queue("\r", "password")
            return
        password_region = plain[self.password_region_start :]
        if self.stage == "password" and PASSWORD_PROMPT_PATTERN.search(password_region):
            self.queue(self.password + "\n", "finish")
            return

    def assert_success(self) -> None:
        plain = self.plain()
        if "Host preparado. O agent ainda não foi configurado" not in plain:
            raise SystemExit("prepare finished without the success message")
        if "Privilégio remoto: su - depois do login SSH" not in plain:
            raise SystemExit("prepare did not take the su path")
        if self.expected_user == "absent" and "ausente, será criado sem sudo" not in plain:
            raise SystemExit("prepare did not describe a new runner user")
        if self.expected_user == "ready" and "já conforme, será mantido sem sudo" not in plain:
            raise SystemExit("prepare did not describe an existing runner user")
        if self.stage != "finish":
            raise SystemExit("prepare finished before the su password step")


def drain(master: int, driver: PrepareDriver) -> None:
    while True:
        ready, _, _ = select.select([master], [], [], 0.2)
        if master not in ready:
            return
        try:
            chunk = os.read(master, 4096)
        except OSError:
            return
        if chunk == b"":
            return
        driver.append(chunk)


def failure_tail(driver: PrepareDriver) -> str:
    lines = driver.plain().splitlines()
    return "\n".join(lines[-80:])


def run_prepare(password: str, transcript_path: str, expected_user: str) -> None:
    project_directory = os.environ["VALIDATION_PROJECT"]
    go_binary = os.environ["VALIDATION_GO"]
    driver = PrepareDriver(password, transcript_path, expected_user)
    child_pid, master = pty.fork()
    if child_pid == 0:
        os.chdir(project_directory)
        os.environ["TERM"] = "xterm-256color"
        os.environ["LANG"] = "C.UTF-8"
        os.execvp(go_binary, [go_binary, "run", "./cmd/prepare"])
    driver.master = master
    set_window_size(master)
    deadline = time.monotonic() + 900
    exit_code = None
    try:
        while time.monotonic() < deadline:
            driver.flush_pending()
            ready, _, _ = select.select([master], [], [], 0.2)
            if master in ready:
                try:
                    chunk = os.read(master, 4096)
                except OSError:
                    chunk = b""
                if chunk == b"":
                    finished_pid, status = os.waitpid(child_pid, 0)
                    drain(master, driver)
                    exit_code = os.waitstatus_to_exitcode(status)
                    break
                driver.append(chunk)
                driver.advance()
            finished_pid, status = os.waitpid(child_pid, os.WNOHANG)
            if finished_pid != 0:
                drain(master, driver)
                exit_code = os.waitstatus_to_exitcode(status)
                break
        if exit_code is None:
            os.kill(child_pid, signal.SIGTERM)
            _, status = os.waitpid(child_pid, 0)
            raise SystemExit(
                f"prepare timed out during {driver.stage}\n{failure_tail(driver)}"
            )
        if exit_code != 0:
            raise SystemExit(
                f"prepare exited {exit_code} during {driver.stage}\n{failure_tail(driver)}"
            )
        try:
            driver.assert_success()
        except SystemExit as error:
            raise SystemExit(f"{error}\n{failure_tail(driver)}") from error
    finally:
        try:
            os.close(master)
        except OSError:
            pass


def main() -> None:
    password = read_password(os.environ["VALIDATION_PASSWORD_FILE"])
    transcript_path = os.environ["VALIDATION_TRANSCRIPT"]
    expected_user = os.environ["VALIDATION_EXPECT_USER"]
    if expected_user not in {"absent", "ready"}:
        raise SystemExit("unexpected user expectation")
    with open(transcript_path, "a", encoding="utf-8") as handle:
        handle.write(f"\n=== run expected_user={expected_user} ===\n")
    run_prepare(password, transcript_path, expected_user)


if __name__ == "__main__":
    main()
