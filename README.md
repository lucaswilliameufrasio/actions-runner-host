# Actions Runner Host

CLI para preparar uma VPS antes de instalar o agent self-hosted do GitHub Actions. Cobre Debian, Ubuntu, RHEL e derivados, Fedora, Amazon Linux e SUSE.

O preparo cria um usuário sem sudo, o diretório do runner e os pacotes de runtime. O registro do agent fica para o passo seguinte, manual, sem token nesta ferramenta.

```sh
mise install
make check
make prepare
```

O alias SSH é pedido em toda execução. Não há host padrão. O passo a passo, os limites e a configuração posterior do agent estão em [`PREPARE.md`](PREPARE.md). Esta CLI continua sendo o preparo sem control plane. O `runner-fleet` agora também prepara o host e administra runners persistentes; a fronteira entre os dois está em [`docs/ADR.md`](docs/ADR.md).

`make check` roda `go vet`, `go test` e confere `gofmt`. Ele não usa Docker.

`make validate-images` percorre as releases em [`validation/images.txt`](validation/images.txt): Debian, Ubuntu, Rocky Linux, AlmaLinux, Oracle Linux, CentOS Stream, Fedora, Amazon Linux e openSUSE Leap. Cada imagem sobe com systemd e SSH de usuário comum, e a CLI prepara o host duas vezes pelo `su -`. `make validate-debian` roda só o Debian 13. Outra imagem entra com `make validate-image BASE_IMAGE=rockylinux:9`. O agent não é registrado e a VPS não entra nesse teste.
