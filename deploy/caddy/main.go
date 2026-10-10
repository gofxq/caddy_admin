package main

import (
	_ "github.com/caddy-dns/cloudflare"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/gofxq/caddy_admin/caddy/observability"
)

func main() { caddycmd.Main() }
