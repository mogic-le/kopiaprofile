package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// newDisplayCmd implements `kopiaprofile display` which prints the
// resolved configuration of every profile (post-inheritance, with
// secrets masked).
func newDisplayCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "display [<profile>]",
		Short: "Display the resolved configuration for a profile (or all profiles)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			names := cfg.Names()
			if len(args) == 1 {
				names = []string{args[0]}
			}
			for _, n := range names {
				prof, ok := cfg.Get(n)
				if !ok {
					return errorf("unknown profile %q", n)
				}
				Print("== profile: %s ==", n)
				printProfile(prof)
				Print("")
			}
			return nil
		},
	}
	return cmd
}

func printProfile(p config.Profile) {
	Print("  description    : %s", p.Description)
	Print("  inherit        : %s", p.Inherit)
	Print("  initialize     : %v", p.Initialize)
	Print("  kopia-binary   : %s", p.KopiaBinary)
	Print("  cache-dir      : %s", p.CacheDir)
	Print("  password       : source=%s service=%s", p.Password.Source, p.Password.KeyringService)
	Print("  repository     : type=%s bucket=%s region=%s prefix=%s",
		p.Repository.Type, p.Repository.Bucket, p.Repository.Region, p.Repository.Prefix)
	if !p.Repository.ObjectLock.IsZero() {
		Print("  object-lock    : mode=%s retention=%s extend-on-maint=%v",
			p.Repository.ObjectLock.Mode,
			p.Repository.ObjectLock.RetentionPeriod,
			p.Repository.ObjectLock.ExtendOnMaintenance)
	}
	Print("  sources        : %s", strings.Join(p.Backup.Sources, ", "))
	Print("  tags           : %s", strings.Join(p.Backup.Tags, ", "))
	Print("  retention      : latest=%d daily=%d weekly=%d monthly=%d annual=%d",
		p.Retention.KeepLatest, p.Retention.KeepDaily, p.Retention.KeepWeekly,
		p.Retention.KeepMonthly, p.Retention.KeepAnnual)
	Print("  run-before     : %s", p.RunBefore)
	Print("  run-after      : %s", p.RunAfter)
	Print("  run-after-fail : %s", p.RunAfterFail)
	Print("  run-finally    : %s", p.RunFinally)
	if len(p.Env) > 0 {
		Print("  env (masked)   :")
		for k, v := range config.MaskMap(p.Env) {
			Print("    %s = %s", k, v)
		}
	}
}

// short helper: indented heading print
func init() {
	// no-op; the function above is exported via package config.
	_ = fmt.Sprintf
}
