package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"tamir-go-bnc/internal/fxbot"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address (default picks a free port)")
	dataDir := flag.String("data", "data", "directory for trades ledger + exports")
	pepperDir := flag.String("pepperstone", "pepperstone-ctrader-python", "folder containing the Pepperstone python scripts and venv")
	openBrowser := flag.Bool("open-browser", true, "open the UI in the default browser on start")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}

	hub := fxbot.NewHub()
	logbuf := fxbot.NewLogBuf(1000)
	bot := fxbot.NewBot(*dataDir, *pepperDir, hub, logbuf)
	srv := fxbot.NewServer(bot, hub)

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", *addr, err)
	}
	url := fmt.Sprintf("http://%s/", listener.Addr().String())

	log.Printf("FX trading bot UI: %s", url)
	log.Printf("data dir: %s | pepperstone scripts: %s", *dataDir, *pepperDir)

	if *openBrowser {
		openInBrowser(url)
	}

	go func() {
		if err := http.Serve(listener, srv.Handler()); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("shutting down…")
	bot.Stop()
}

func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser automatically: %v", err)
	}
}
