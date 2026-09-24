// Command apheliondmm-transactions-migrate validates a V3 store and creates a
// separate V4 transaction-storage destination. It never rewrites the source.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"

	"sdmm/internal/aphelion/collab/store/postgres"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

const postgresDSNEnvironment = "APHELION_POSTGRES_DSN"

type options struct {
	sqliteSource      string
	sqliteDestination string
	postgresSource    string
	postgresTarget    string
	postgresDSN       string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	configuration, err := parseOptions(args, getenv, stderr)
	if err != nil {
		return err
	}
	if configuration.sqliteSource != "" {
		if err := sqlite.UpgradeTransactions(ctx, configuration.sqliteSource, configuration.sqliteDestination); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "Created validated V4 SQLite destination %q. V3 source %q remains unchanged; this is not an off-host backup.\n", configuration.sqliteDestination, configuration.sqliteSource)
		return err
	}
	if err := postgres.UpgradeTransactions(ctx, postgres.Config{DSN: configuration.postgresDSN, Schema: configuration.postgresSource}, postgres.Config{DSN: configuration.postgresDSN, Schema: configuration.postgresTarget}); err != nil {
		return redactPostgresError(err, configuration.postgresDSN)
	}
	_, err = fmt.Fprintf(stdout, "Created validated V4 PostgreSQL destination schema %q. V3 source schema %q remains retained; it is not an off-host disaster backup.\n", configuration.postgresTarget, configuration.postgresSource)
	return err
}

func redactPostgresError(err error, dsn string) error {
	message := err.Error()
	message = strings.ReplaceAll(message, dsn, "[redacted PostgreSQL DSN]")
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.User != nil {
		if password, present := parsed.User.Password(); present && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
		}
	}
	for _, field := range strings.Fields(dsn) {
		key, value, present := strings.Cut(field, "=")
		if present && strings.EqualFold(key, "password") {
			value = strings.Trim(value, `"'`)
			if value != "" {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	return errors.New(message)
}

func parseOptions(args []string, getenv func(string) string, stderr io.Writer) (options, error) {
	var configuration options
	flags := flag.NewFlagSet("apheliondmm-transactions-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configuration.sqliteSource, "sqlite-source", "", "existing SQLite V3 database path (read-only source)")
	flags.StringVar(&configuration.sqliteDestination, "sqlite-destination", "", "new SQLite V4 destination path (must not exist)")
	flags.StringVar(&configuration.postgresSource, "postgres-source-schema", "", "existing PostgreSQL V3 source schema")
	flags.StringVar(&configuration.postgresTarget, "postgres-destination-schema", "", "new PostgreSQL V4 destination schema (must not exist)")
	flags.Usage = func() {
		if _, err := fmt.Fprintln(stderr, "Validate a V3 store and stage a distinct V4 copy. The source is retained; this command does not switch application configuration."); err != nil {
			return
		}
		if _, err := fmt.Fprintf(stderr, "PostgreSQL credentials are read from %s and are never printed.\n\n", postgresDSNEnvironment); err != nil {
			return
		}
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments")
	}
	configuration.postgresDSN = strings.TrimSpace(getenv(postgresDSNEnvironment))
	sqliteSelected := configuration.sqliteSource != "" || configuration.sqliteDestination != ""
	postgresSelected := configuration.postgresSource != "" || configuration.postgresTarget != ""
	if sqliteSelected == postgresSelected {
		return options{}, fmt.Errorf("select exactly one migration backend: SQLite paths or PostgreSQL source/destination schemas")
	}
	if sqliteSelected && (configuration.sqliteSource == "" || configuration.sqliteDestination == "") {
		return options{}, fmt.Errorf("both --sqlite-source and --sqlite-destination are required")
	}
	if postgresSelected {
		if configuration.postgresSource == "" || configuration.postgresTarget == "" {
			return options{}, fmt.Errorf("both --postgres-source-schema and --postgres-destination-schema are required")
		}
		if configuration.postgresDSN == "" {
			return options{}, fmt.Errorf("%s must contain the PostgreSQL connection string", postgresDSNEnvironment)
		}
	}
	return configuration, nil
}
