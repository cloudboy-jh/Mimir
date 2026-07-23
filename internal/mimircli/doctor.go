package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func doctor(ctx context.Context, args []string, out io.Writer) error {
	jsonOutput := false
	for _, arg := range args {
		if arg != "--json" {
			return fmt.Errorf("usage: mimir doctor [--json]")
		}
		jsonOutput = true
	}
	report := runDoctor(ctx)
	if jsonOutput {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	for _, check := range report.Checks {
		fmt.Fprintf(out, "%s  %s", check.Status, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(out, " · %s", check.Detail)
		}
		if check.Repair != "" {
			fmt.Fprintf(out, " · repair: %s", check.Repair)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func runDoctor(ctx context.Context) doctorReport {
	report := doctorReport{OK: true}
	add := func(name, status, detail, repair string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Detail: detail, Repair: repair})
		if status == "failed" {
			report.OK = false
		}
	}
	artifacts, artifactErr := checkManagedArtifacts()
	if artifactErr != nil {
		add("managed-artifacts", "failed", artifactErr.Error(), "mimir install or mimir update")
	} else {
		for _, artifact := range artifacts.Artifacts {
			status := "ok"
			repair := ""
			if artifact.Status != artifactCurrent {
				status = "failed"
				repair = "mimir install or mimir update"
			}
			add("managed-artifact "+artifact.Source, status, string(artifact.Status)+" · "+artifact.Path, repair)
		}
	}
	pointer, err := loadPointer()
	if err != nil {
		add("connection", "failed", err.Error(), "mimir login")
		return report
	}
	if _, err := remoteRequestWithPointer(ctx, pointer, "GET", "/whoami", nil); err != nil {
		add("worker", "failed", err.Error(), "mimir login")
	} else {
		add("worker", "ok", pointer.URL, "")
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		add("connection.manifest", "failed", err.Error(), "mimir login")
		return report
	}
	add("opencode", "skipped", "managed artifacts do not rewrite OpenCode configuration", "")
	checkHermesIntegration(ctx, add, pointer, manifest)
	return report
}

func checkHermesIntegration(ctx context.Context, add func(string, string, string, string), pointer Pointer, manifest connectionManifest) {
	hermesHome, hermesFound, err := discoverHermesHome()
	if err != nil {
		add("hermes.home", "failed", err.Error(), "")
		return
	}
	if !hermesFound {
		add("hermes", "skipped", "Hermes is not installed", "")
		return
	}
	if matches, detail := hermesIntegrationMatches(hermesHome, manifest); !matches {
		add("hermes.openrouter", "failed", detail, "mimir update")
	} else {
		add("hermes.openrouter", "ok", detail, "")
	}
	hermesKey, err := hermesOpenRouterKey(hermesHome)
	if err != nil {
		add("hermes.credential", "failed", err.Error(), "configure Hermes OpenRouter authentication")
		return
	}
	hermesPointer := Pointer{URL: pointer.URL, Token: hermesKey}
	for _, endpoint := range []string{"models", "key", "credits"} {
		path := "/v1/hermes/" + endpoint
		if _, err := remoteRequestWithPointer(ctx, hermesPointer, "GET", path, nil); err != nil {
			add("hermes."+endpoint, "failed", err.Error(), "mimir deploy")
		} else {
			add("hermes."+endpoint, "ok", path, "")
		}
	}
}
