package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseOptionsRequiresOneCompleteBackend(t *testing.T) {
	getenv := func(string) string { return "" }
	for _, args := range [][]string{
		{},
		{"--sqlite-source=source.db"},
		{"--postgres-source-schema=legacy"},
		{"--sqlite-source=source.db", "--sqlite-destination=copy.db", "--postgres-source-schema=legacy", "--postgres-destination-schema=staged"},
	} {
		var stderr bytes.Buffer
		if _, err := parseOptions(args, getenv, &stderr); err == nil {
			t.Errorf("parseOptions(%q) succeeded; want a backend validation error", args)
		}
	}
}

func TestParseOptionsReadsPostgresDSNFromEnvironment(t *testing.T) {
	const secretDSN = "postgres://migration-user:private-value@localhost/db"
	var stderr bytes.Buffer
	options, err := parseOptions([]string{"--postgres-source-schema=legacy", "--postgres-destination-schema=staged"}, func(key string) string {
		if key == postgresDSNEnvironment {
			return secretDSN
		}
		return ""
	}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if options.postgresDSN != secretDSN {
		t.Fatal("PostgreSQL DSN was not read from the configured environment variable")
	}
	if strings.Contains(stderr.String(), secretDSN) {
		t.Fatal("PostgreSQL DSN was printed during option parsing")
	}
	redacted := redactPostgresError(errors.New("connect failed for "+secretDSN), secretDSN).Error()
	if strings.Contains(redacted, secretDSN) || strings.Contains(redacted, "private-value") {
		t.Fatalf("PostgreSQL error retained connection credentials: %q", redacted)
	}
}
