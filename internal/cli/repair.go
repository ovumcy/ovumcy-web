package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// repairUsage lists the repairs this binary knows. It is what an operator
// reaches from a refused migration, so every line has to name a command that
// runs on a STOPPED instance — the refusal is why there is no running one.
const repairUsage = `usage: ovumcy repair <repair> [--apply]

Repairs run against the database directly and do not need the server. Each one
inspects and reports by default, and changes nothing until --apply is passed.
Take a backup before --apply.

  symptom-names   Merge symptoms one account holds twice under the same name,
                  which is what stops migration 037 from creating its index`

// RunRepairCommand is the operator entry point for offline data repairs.
func RunRepairCommand(databaseConfig db.Config, args []string) error {
	return runRepairCommand(databaseConfig, args, os.Stdout)
}

func runRepairCommand(databaseConfig db.Config, args []string, output io.Writer) error {
	if output == nil {
		output = os.Stdout
	}
	if len(args) == 0 {
		return errors.New(repairUsage)
	}

	repair := strings.ToLower(strings.TrimSpace(args[0]))
	if repair != "symptom-names" {
		return errors.New(repairUsage)
	}

	apply, err := parseRepairApplyFlag(args[1:])
	if err != nil {
		return err
	}

	// Without migrations, deliberately: this repair exists for a database a
	// migration refuses, so opening it the ordinary way would meet that refusal
	// and fail exactly where the boot already fails. symptom_types and
	// daily_logs have both been there since migration 001/003, far below
	// anything that can still be pending.
	database, err := db.OpenDatabaseWithoutMigrations(databaseConfig)
	if err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("database init failed: %w", err) // codecov:ignore -- defensive: (*gorm.DB).DB() only errors when the pool is unavailable, which cannot happen on the handle the open just returned
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	repository := db.NewSymptomDuplicateRepository(database)
	// Before anything queries the catalogue, and once for both modes: this is
	// the only command that opens a database of unknown schema version, so "not
	// an Ovumcy database" is a mistake only it can make and only it can name.
	if err := repository.RequireSymptomCatalogue(context.Background()); err != nil {
		return fmt.Errorf("%s: %w%s", describeRepairTarget(databaseConfig), err, repairPreconditionRemedy(err))
	}

	service := services.NewSymptomDuplicateRepairService(repository)
	if apply {
		return runSymptomNamesRepairApply(service, output)
	}
	return runSymptomNamesRepairInspect(service, output)
}

// repairPreconditionRemedy is what to do about each way the precondition can
// fail, which are different mistakes and take different answers.
//
// It advises only on the two verdicts it actually recognises, and says nothing
// otherwise. A default arm here would attach one of them to every failure the
// precondition can ever grow — starting with the one it already has: a database
// that stopped answering carries its own cause, and appending "check your
// DB_PATH" to it would send the operator to audit a connection setting that was
// correct, which is what the schema probe was taught to stop doing.
//
// A schema older than migration 004 is likewise not a wrong database. The way
// forward there is to start the instance, which carries the schema up; that may
// stop again on migration 037, and this command is then the answer, now with
// the column it needs, so the loop closes rather than sending the operator in a
// circle.
func repairPreconditionRemedy(err error) string {
	switch {
	case errors.Is(err, db.ErrSymptomCatalogueTooOld):
		return ". Start the instance once to carry the schema forward; if it stops on migration 037, run this repair again"
	case errors.Is(err, db.ErrSymptomCatalogueAbsent):
		return ". Point DB_DRIVER and DB_PATH (or DATABASE_URL) at the instance's own database — a SQLite path that does not exist is not refused, the driver creates an empty database there"
	default:
		return ""
	}
}

// describeRepairTarget names the database the operator actually reached, so a
// mistyped path is visible in the refusal rather than inferred.
//
// A Postgres URL carries credentials, so it is named by its variable and never
// by its value. The SQLite path is the whole point of the message and is not a
// secret.
func describeRepairTarget(databaseConfig db.Config) string {
	if databaseConfig.Driver == db.DriverPostgres {
		return "the postgres database DATABASE_URL points at"
	}
	return "sqlite database " + strings.TrimSpace(databaseConfig.SQLitePath)
}

func parseRepairApplyFlag(args []string) (bool, error) {
	apply := false
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "--apply":
			apply = true
		default:
			return false, errors.New(repairUsage)
		}
	}
	return apply, nil
}

// errSymptomNameDuplicatesRemain is what the inspection answers with when the
// database is not ready for the migration. It is an error rather than a plain
// report so the command can gate an upgrade script: a zero exit means this
// instance will boot, a non-zero one means it will not.
var errSymptomNameDuplicatesRemain = errors.New("this database is not ready for migration 037: run `ovumcy repair symptom-names --apply` to merge the groups above, after taking a backup")

func runSymptomNamesRepairInspect(service *services.SymptomDuplicateRepairService, output io.Writer) error {
	groups, err := service.Inspect(context.Background())
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		_, _ = fmt.Fprintln(output, "No account holds two symptoms under one name. Migration 037 has nothing to refuse.")
		return nil
	}

	writeSymptomDuplicatePlan(output, services.PlanSymptomDuplicateMerges(groups))
	return errSymptomNameDuplicatesRemain
}

func runSymptomNamesRepairApply(service *services.SymptomDuplicateRepairService, output io.Writer) error {
	merges, outcome, err := service.Repair(context.Background())
	if err != nil {
		return err
	}
	if len(merges) == 0 {
		_, _ = fmt.Fprintln(output, "No account holds two symptoms under one name. Nothing to merge.")
		return nil
	}

	writeSymptomDuplicatePlan(output, merges)
	_, _ = fmt.Fprintf(
		output,
		"\nMerged %d group(s): %d symptom row(s) removed, %d day log(s) re-pointed at the kept symptom.\n"+
			"Start the instance to apply migration 037.\n",
		outcome.GroupsMerged, outcome.SymptomsRemoved, outcome.DailyLogsRewritten,
	)
	return nil
}

// writeSymptomDuplicatePlan prints what will be kept and what will be absorbed,
// per account. The same table serves the inspection and the applied run on
// purpose: the operator reads the plan before consenting, and then reads that
// the run carried out the plan they read.
func writeSymptomDuplicatePlan(output io.Writer, merges []models.SymptomMerge) {
	_, _ = fmt.Fprintf(
		output,
		"%d account/name group(s) hold more than one symptom, as this database's own lower() folds names:\n\n",
		len(merges),
	)

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "ACCOUNT\tACTION\tSYMPTOM ID\tNAME\tKIND\tSTATE")
	for _, merge := range merges {
		writeSymptomDuplicateRow(writer, merge.UserID, "keep", merge.Survivor)
		for _, absorbed := range merge.Absorbed {
			writeSymptomDuplicateRow(writer, merge.UserID, "merge into kept", absorbed)
		}
	}
	_ = writer.Flush()
}

func writeSymptomDuplicateRow(writer io.Writer, userID uint, action string, symptom models.SymptomType) {
	kind := "custom"
	if symptom.IsBuiltin {
		kind = "built-in"
	}
	state := "active"
	if !symptom.IsActive() {
		state = "archived"
	}
	_, _ = fmt.Fprintf(writer, "%d\t%s\t%d\t%s\t%s\t%s\n", userID, action, symptom.ID, symptom.Name, kind, state)
}
