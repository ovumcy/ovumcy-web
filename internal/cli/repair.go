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

	service := services.NewSymptomDuplicateRepairService(db.NewSymptomDuplicateRepository(database))
	if apply {
		return runSymptomNamesRepairApply(service, output)
	}
	return runSymptomNamesRepairInspect(service, output)
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
