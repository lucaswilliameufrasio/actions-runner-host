# Actions Runner Host

CLI para preparar uma VPS Debian ou Ubuntu antes de instalar o agent self-hosted do GitHub Actions.

O preparo cria um usuário sem sudo, o diretório do runner e os pacotes de runtime. O registro do agent fica para o passo seguinte, manual, sem token nesta ferramenta.

```sh
mise install
make check
make prepare
```

O alias SSH é pedido em toda execução. Não há host padrão. O passo a passo, os limites e a configuração posterior do agent estão em [`PREPARE.md`](PREPARE.md). A decisão de não usar o `runner-fleet` neste passo está em [`docs/ADR.md`](docs/ADR.md).

`make check` roda `go vet`, `go test` e confere `gofmt`. Ele não usa Docker.

`make validate-images` percorre Debian 12, Debian 13, Ubuntu 22.04, Ubuntu 24.04 e Ubuntu 26.04. Cada imagem sobe com systemd e SSH de usuário comum, e a CLI prepara o host duas vezes pelo `su -`. `make validate-debian` roda só o Debian 13. Outra imagem da família Debian entra com `make validate-image BASE_IMAGE=ubuntu:24.04`. O agent não é registrado e a VPS não entra nesse teste.
