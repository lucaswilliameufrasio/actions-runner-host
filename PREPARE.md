# Preparo da VPS para o runner self-hosted

Esta CLI prepara um host Debian ou Ubuntu antes do agent oficial do GitHub Actions. Ela não baixa o runner, não pede token e não executa `config.sh` nem `svc.sh`.

Nenhum alias SSH fica gravado no repositório.

## Pré-requisitos

- Go 1.27.3 gerenciado pelo mise e `ssh` no computador de onde o preparo será executado.
- Um alias SSH em `~/.ssh/config`, autenticado por chave. Exemplo genérico:

  ```sshconfig
  Host minha-vps
      HostName <IP-ou-hostname>
      User <usuario-administrativo>
      IdentityFile ~/.ssh/<chave>
  ```

- A conta do alias entra como root, tem sudo sem senha (`sudo -n`), ou é um usuário comum cujo root abre com `su -`. A CLI não cria sudoers e não grava senha.
- A VPS é Debian ou Ubuntu, com systemd. Tailscale é informativo: o acesso administrativo continua pelo alias.

## Executar

No checkout:

```sh
make prepare
```

1. Informe o alias SSH.
2. Informe o usuário do runner. Enter vazio usa `github-runner`.
3. A CLI consulta a VPS sem alterá-la e mostra o plano.
4. Escolha **aplicar**. Esc ou **cancelar** não muda a máquina.

No Debian em que o SSH entra com um usuário comum, o plano mostra `su - depois do login SSH`. Depois da confirmação, o `su -` pede a senha de root uma vez neste terminal. A senha não vai para argumento, arquivo ou log. O preparo e a verificação rodam nessa mesma sessão. Os scripts temporários ficam em `~/.local/state/actions-runner-host` no usuário do SSH e são apagados ao terminar.

O preparo cria o usuário sem shell de login, com home `0700` em `/var/lib/<usuário>` e o diretório `/var/lib/<usuário>/actions-runner`. Instala `ca-certificates`, `curl`, `tar`, `git`, `libkrb5-3`, `zlib1g` e o `libssl`, `liblttng` e `libicu` que existirem na distro. `apt-get update` não faz upgrade geral e não reinicia serviços.

Se o usuário já existe e o home, o shell e os grupos conferem, a execução reaproveita a conta. Se o home for outro, o shell permitir login, ou a conta estiver em `sudo`, `admin`, `wheel` ou `docker`, a CLI para e não modifica essa conta.

## Configurar o agent depois

A CLI imprime a arquitetura (`x64` ou `arm64`) e o diretório. Como root:

1. Baixe o tarball em [actions/runner releases](https://github.com/actions/runner/releases) e confira o SHA256 publicado lá.
2. Extraia em `/var/lib/<usuário>/actions-runner` e entregue o diretório ao usuário.
3. Rode `./config.sh` com `runuser -u <usuário>`, numa URL de repositório privado e um token de registro de curta duração.
4. Rode `./svc.sh install <usuário>` e `./svc.sh start`.
5. Confira que o unit ficou com `User=<usuário>` e que `sudo -n -l -U <usuário>` continua recusado.

Não registre o runner num repositório público. Workflow de fork também executaria código nessa VPS.

## O que não é alterado

Firewall, ACLs do Tailscale, DNS, Nginx, registro no GitHub e sudoers. A CLI também não coloca o usuário no grupo `docker` e não instala Docker.

## Validação local

`make validate-images` repete o fluxo em Debian 12, Debian 13, Ubuntu 22.04, Ubuntu 24.04 e Ubuntu 26.04. O comando sobe um container com systemd, entra por SSH num usuário comum e usa `su -`. Não registra o agent. `make check` não depende desse teste. A lista fechada está em `validation/images.txt`.

## Diagnóstico

- **Alias SSH não conecta:** teste `ssh <alias>` e a chave em `~/.ssh/config`. A CLI usa `BatchMode` e não cai para senha.
- **Senha do su recusada:** a VPS não chega a ser alterada. Rode de novo e use a senha de root. Não dê sudo ao usuário do runner.
- **Usuário em conflito:** a conta foi criada fora deste contrato. A CLI não a apaga nem a converte.
- **Saída para o GitHub falhou:** o usuário ainda pode ter sido preparado. O aviso aparece no final; o agent não foi registrado.
