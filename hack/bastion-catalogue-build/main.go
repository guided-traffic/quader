// Command bastion-catalogue-build measures the builder of the Bastions' public catalogue in process (W-35's package P6): it runs a
// replica of the role worker against an instance of the UI development profile, with the switches gui and docs on for every
// registration (app.Options.BastionSwitches, which only a test or this measurement sets), runs the job bastion-catalogue once
// whole and once again after one package's fingerprint changed, and prints each run's time and result. It is a measurement, not a
// test, and nothing of it enters the product; docs/developer/bastion.md says how it was run and has its numbers.
//
//	go run ./hack/bastion-catalogue-build -config .dev/ui-dev-<n>/quader.yaml -pause
//
// The instance's own quader must not run meanwhile (it claims jobs too, with the switches off). The instance needs a registration
// with a reported base URL that server.additional_base_urls lists (QUADER_SERVER_ADDITIONAL_BASE_URLS in the environment).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/jobs"
)

func main() {
	file := flag.String("config", "", "the instance's quader.yaml")
	pause := flag.Bool("pause", false, "after the first run, wait for a line on standard input (change a package's facts meanwhile) and run again")
	flag.Parse()
	if err := run(*file, *pause); err != nil {
		fmt.Fprintln(os.Stderr, "bastion-catalogue-build:", err)
		os.Exit(1)
	}
}

func run(file string, pause bool) error {
	loaded, err := config.Load(config.Options{File: file, Environ: os.Environ(), Command: config.CommandServe})
	if err != nil {
		return err
	}
	a, err := app.New(app.Options{
		Loaded: loaded, Role: config.RoleWorker, LogWriter: io.Discard,
		BastionSwitches: func(string) iface.Switches { return iface.Switches{GUI: true, Docs: true} },
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		return err
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), time.Minute)
		defer scancel()
		_ = a.Shutdown(sctx)
	}()
	select {
	case <-a.Started():
	case err := <-a.Failed():
		return err
	}
	if err := once(ctx, a, "full"); err != nil {
		return err
	}
	if pause {
		fmt.Println("change the facts of one package now, then press return")
		_, _ = fmt.Scanln()
	}
	return once(ctx, a, "again")
}

// once runs the job and prints its time and result.
func once(ctx context.Context, a *app.App, what string) error {
	q := a.Queue()
	res, err := q.EnqueueNow(ctx, jobs.EnqueueSpec{Kind: app.KindBastionCatalogue})
	if err != nil {
		return err
	}
	for {
		j, ok, err := q.Get(ctx, res.Job.ID)
		if err != nil || !ok {
			return fmt.Errorf("the job %s (found %v): %w", res.Job.ID, ok, err)
		}
		switch j.State {
		case "succeeded", "failed", "cancelled":
			out, _ := json.Marshal(map[string]any{
				"run": what, "state": j.State, "seconds": j.FinishedAt.Sub(j.StartedAt).Seconds(), "result": j.Result,
				"error": j.Error,
			})
			fmt.Println(string(out))
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}
