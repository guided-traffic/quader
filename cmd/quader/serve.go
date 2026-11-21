package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/vuln/scanner"
)

// serveBastion runs one replica of the role bastion (R-BAS-001). The role has a configuration tree of its own,
// so it is chosen here, before the main tree is loaded, by --role and by nothing else: process.role is not a
// key of the Bastion's tree. A key of another section stops startup with an error that names the key and the
// role.
func serveBastion(c *commonFlags, s streams, flags, names map[string]string) error {
	delete(flags, "process.role")
	delete(names, "process.role")
	loaded, err := config.LoadBastion(config.Options{
		File: c.file, Environ: s.environ, Flags: flags, FlagNames: names, Command: config.CommandServe,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // restore default handling: a second signal terminates the process
	}()
	out := s.stdout
	if out == nil {
		out = os.Stdout
	}
	return app.RunBastion(ctx, app.BastionOptions{Loaded: loaded, Environ: s.environ, LogWriter: out})
}

// serveCmd runs one replica of a role (R-ARCH-001) until SIGTERM or SIGINT, then
// shuts down in the order of R-ARCH-024. A second signal ends the process at once.
func serveCmd(args []string, s streams) error {
	c := newFlags("serve", s)
	c.fs.String("role", "", "api, packages, worker, web, all or bastion; overrides process.role")
	flags, names, err := c.parse(args, map[string]string{"role": "process.role", "log-level": "log.level"})
	if err != nil {
		return err
	}
	if flags["process.role"] == config.BastionRole {
		return serveBastion(c, s, flags, names)
	}
	loaded, err := c.loadConfig(s, config.CommandServe, flags, names)
	if err != nil {
		return err
	}
	if loaded.Config.Process.Role == config.RoleScanner {
		// A scanner's process is not dumpable from its start, before it reads a pair or opens a listener (Q-VULN-203): the analysers it
		// starts run as its user and can neither attach to it nor open its memory. Here and not in the app, so that a scanner replica
		// that a test starts in its own process leaves that process as it is.
		if err := scanner.NotDumpable(); err != nil {
			return fmt.Errorf("the scanner cannot make its process not dumpable: %w", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // restore default handling: a second signal terminates the process
	}()
	out := s.stdout
	if out == nil {
		out = os.Stdout
	}
	return app.RunWith(ctx, app.Options{Loaded: loaded, Environ: s.environ, LogWriter: out})
}
