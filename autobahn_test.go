package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"golang.org/x/sync/errgroup"
)

// use autobahn docker container to run test against our echo server
// - starts echo
// - run autobahn docker
// - autobahn runs tests on echo server (tests defined in config/client.json)
// - wait for docker to exit
// - analyze test reports
func TestAutobahn(t *testing.T) {
	address := "localhost:9001"
	cwd, _ := os.Getwd()
	reportsFolder := cwd + "/autobahn/reports/clients"

	// remove all reports
	if err := os.RemoveAll(reportsFolder); err != nil {
		t.Fatalf("remove reports folder %s", err)
	}

	// start echo server
	ctx, stop := context.WithCancel(context.Background())
	var g errgroup.Group
	g.Go(func() error {
		return Serve(ctx, address, echo)
	})

	runContainer(t, cwd)

	// stop echo server and get error if any
	stop()
	if err := g.Wait(); err != nil {
		t.Fatalf("echo server failed %s", err)
	}

	analyzeReports(t, reportsFolder)
}

func runContainer(t *testing.T, cwd string) {
	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		t.Fatalf("Could not connect to Docker: %s", err)
	}

	opts := dockertest.RunOptions{
		Repository: "crossbario/autobahn-testsuite",
		Tag:        "0.8.2",
		Cmd:        []string{"wstest", "--mode", "fuzzingclient", "--spec", "/config/client.json"},
		Mounts: []string{
			cwd + "/autobahn/config:/config",
			cwd + "/autobahn/reports:/reports",
		},
	}
	// pulls an image, creates a container based on it and runs it
	resource, err := pool.RunWithOptions(&opts)
	if err != nil {
		t.Fatalf("Could not start resource: %s", err)
	}
	t.Logf("container started, id: %s", resource.Container.ID[:12])

	// wait for container to exit
	for {
		ic, err := pool.Client.InspectContainer(resource.Container.ID)
		if err != nil {
			t.Fatalf("inspect container error %s", err)
		}
		if !ic.State.Running {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	if err := pool.Purge(resource); err != nil {
		t.Fatalf("Could not purge resource: %s", err)
	}
}

func analyzeReports(t *testing.T, reportsFolder string) {
	files, err := filepath.Glob(reportsFolder + "/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "index.json") {
			continue
		}
		buf, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var c Case
		json.Unmarshal(buf, &c)

		t.Logf("%-7s %s %s", c.ID, c.Behavior, c.BehaviorClose)
		if c.Behavior == "INFORMATIONAL" {
			continue
		}
		if c.Behavior != "OK" || c.BehaviorClose != "OK" {
			t.Fail()
		}
	}
}

type Case struct {
	ID            string
	Case          int
	Agent         string
	Behavior      string
	BehaviorClose string
}
