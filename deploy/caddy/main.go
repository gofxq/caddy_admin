package main

import (
	"fmt"
	"os"

	_ "github.com/caddy-dns/cloudflare"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/gofxq/caddy_admin/caddy/observability"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "manager-adapt-http-route" {
		if err := runRouteAdapter(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stdout, err)
			os.Exit(2)
		}
		return
	}
	caddycmd.Main()
}
