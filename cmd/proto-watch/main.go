package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bkcarlos/ProtoWatch/internal/buildinfo"
	"github.com/bkcarlos/ProtoWatch/internal/server"
	"github.com/bkcarlos/ProtoWatch/internal/someip"
	"github.com/bkcarlos/ProtoWatch/internal/update"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	iface := flag.String("interface", "", "start live capture on this Linux interface")
	pcapDir := flag.String("pcap-dir", ".", "directory of server-side PCAP files (default: working directory)")
	read := flag.String("read", "", "stream a classic PCAP file (no total file size limit)")
	cert := flag.String("tls-cert", "", "optional TLS certificate PEM")
	key := flag.String("tls-key", "", "optional TLS private key PEM")
	version := flag.Bool("version", false, "print version")
	mapping := flag.String("someip-map", "", "optional SOME/IP JSON name mapping and ports")
	maxImport := flag.Int64("max-import-mib", 4096, "maximum browser PCAP upload size in MiB")
	flag.Parse()
	if *version {
		fmt.Printf("proto-watch %s (%s)\n", buildinfo.Version, buildinfo.Commit)
		return
	}
	if *iface != "" && *read != "" {
		log.Fatal("choose --interface or --read")
	}
	if (*cert == "") != (*key == "") {
		log.Fatal("--tls-cert and --tls-key must be used together")
	}
	_, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}
	if *maxImport < 1 || *maxImport > (1<<53-1)/(1024*1024) {
		log.Fatal("--max-import-mib must be between 1 and 8589934591")
	}
	restart := make(chan struct{})
	s := server.New()
	s.Updater = update.New(buildinfo.Version, func() { close(restart) })
	s.MaxImportBytes = *maxImport * 1024 * 1024
	if err = s.SetPCAPDir(*pcapDir); err != nil {
		log.Fatal(err)
	}
	if *mapping != "" {
		f, e := os.Open(*mapping)
		if e != nil {
			log.Fatal(e)
		}
		data, e := io.ReadAll(io.LimitReader(f, someip.MaxConfigBytes+1))
		f.Close()
		if e != nil {
			log.Fatal(e)
		}
		if e = s.Store.SetSomeIPConfig(data); e != nil {
			log.Fatal(e)
		}
	}

	if *read != "" {
		if err = s.StartFile(*read); err != nil {
			log.Fatal(err)
		}
	}
	if *iface != "" {
		if err = s.Start(*iface); err != nil {
			log.Fatal(err)
		}
	}
	srv := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-restart:
		}
		s.Stop()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	scheme := "http"
	if *cert != "" {
		scheme = "https"
	}
	display := *listen
	if strings.HasPrefix(display, "0.0.0.0:") {
		display = "<device-ip>" + strings.TrimPrefix(display, "0.0.0.0")
	}
	log.Printf("ProtoWatch: %s://%s — passive observation, Ctrl+C to stop", scheme, display)
	if *cert != "" {
		err = srv.ListenAndServeTLS(*cert, *key)
	} else {
		err = srv.ListenAndServe()
	}
	s.Stop()
	select {
	case <-restart:
		<-shutdownDone
		if err := s.Updater.Restart(); err != nil {
			log.Fatalf("restart failed; start the new binary manually: %v", err)
		}
		return
	default:
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
