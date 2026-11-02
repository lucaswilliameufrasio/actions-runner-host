# Preparo da VPS antes do agent

Status: aceito
Data: 2026-11-01

## Contexto

Um host da tailnet precisa receber um runner self-hosted do GitHub Actions. O `runner-fleet` é um control plane completo, ainda sem homologação contra GitHub e Docker reais. Para uma máquina, o caminho escolhido é o registro manual do agent oficial.

O processo que executa o job não pode ser root nem ter sudo. Um sudo "limitado" continua sendo execução de código arbitrário do workflow.

## Decisão

A CLI `cmd/prepare` só prepara a VPS:

- pede o alias SSH em `~/.ssh/config` e o nome do usuário; não há host embutido;
- exige terminal interativo e confirmação antes de alterar a máquina;
- cria ou reaproveita um usuário de sistema com shell nologin, home `0700` em `/var/lib/<usuário>` e diretório `actions-runner`;
- recusa o preparo se esse usuário já existe fora desse contrato, está em `sudo`/`admin`/`wheel`/`docker`, ou consegue usar `sudo`;
- instala os pacotes de runtime do runner na família da distro, sem upgrade geral e sem reiniciar serviços;
- para antes de baixar o actions/runner, rodar `config.sh` ou `svc.sh`, e não recebe token.

O registro do agent continua manual, só em repositório privado, com o unit oficial usando esse usuário.

## Alternativas rejeitadas

- Usar o `runner-fleet` para um único host. Ele exige App, painel, mTLS e runners efêmeros, e o próprio status do projeto diz que isso não foi exercitado em produção.
- Registrar o runner dentro desta CLI. Mistura preparo de sistema com credencial de curta duração e dificulta parar no limite pedido.
- Rodar o agent como root ou com sudoers parcial. O job escolhe o comando.

## Consequências

- A conta SSH pode ser root, ter sudo sem senha, ou ser o usuário comum do Debian que abre o root com `su -`. A CLI não cria regra sudoers e não grava a senha. Veja a atualização abaixo.
- Só Debian e Ubuntu com systemd. Outras famílias param antes de qualquer mudança. Esta consequência foi substituída pela atualização de famílias de servidor.
- Rodar de novo é idempotente para um usuário já conforme e não apaga um usuário em conflito.
- Quando houver várias máquinas, runners efêmeros ou isolamento de código não confiável, o canário do `runner-fleet` volta a ser o caminho a revisar. Esta CLI não é essa fronteira.

## Atualização — root via su -

Status: aceito
Data: 2026-11-01

Substitui a consequência que exigia SSH como root ou sudo sem senha.

No Debian o login SSH costuma ser um usuário comum, e o root só aparece depois de `su -`. A CLI detecta a ausência de `sudo -n`. Se `su` existe, o plano avisa e, após a confirmação, abre uma sessão `su - -c` com TTY. A senha fica só no prompt do `su`. O preparo e a verificação usam essa única sessão. Continua sem sudoers para o usuário do runner.

## Atualização — famílias de servidor

Status: aceito
Data: 2026-11-01

Substitui a consequência que limitava o preparo a Debian e Ubuntu.

O agent oficial instala dependências em três famílias. A CLI segue essa fronteira:

- `apt`, quando existe `/etc/debian_version` (Debian e Ubuntu);
- `rpm`, quando existe `/etc/redhat-release`, ou quando `ID`/`ID_LIKE` identifica Amazon Linux, Fedora, RHEL ou outro derivado. O Amazon Linux 2023 não publica `/etc/redhat-release`;
- `zypper`, quando `ID` ou `ID_LIKE` contém `suse` (openSUSE e SLES).

Os nomes dos pacotes acompanham o `installdependencies.sh` do actions/runner, com descoberta da versão de `libssl`/`libopenssl`, `liblttng` e `libicu`. No zypper o runtime do LTTng é o primeiro nome existente entre `lttng-ust`, `liblttng-ust1` e `liblttng-ust0`. Em rpm, `curl-minimal` já satisfaz o comando `curl`, o EL 10 entrega `zlib` pelo pacote `zlib-ng-compat`, e `lttng-ust` pode estar num repositório opcional; a CLI habilita `crb`, `powertools` ou CodeReady se o repositório já estiver definido. O contrato do usuário, a ausência de sudoers e a parada antes do registro continuam iguais.

Alpine fica de fora: o binário oficial do runner é glibc e essa plataforma não está na lista suportada. Imagens de RHEL e SLES que exigem assinatura não entram no teste Docker; Rocky, AlmaLinux, Oracle Linux, CentOS Stream e openSUSE Leap cobrem os mesmos caminhos de pacote.
