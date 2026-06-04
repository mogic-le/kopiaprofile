package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// skeletonConfig is the Go representation of the skeleton. The same
// data is rendered to YAML, TOML or JSON depending on the --format
// flag. Keeping a Go value (instead of a string literal) means the
// skeleton cannot drift away from the real struct schema.
func skeletonConfig() *config.File {
	yes := true
	return &config.File{
		Version: "1",
		Global: config.Global{
			DefaultConfig: true,
			KopiaBinary:   "kopia",
			LogLevel:      "info",
		},
		Profiles: map[string]config.Profile{
			"default-base": {
				Description: "Default base; inherit from this",
				Initialize:  true,
				Repository: config.Repository{
					Type:     "s3",
					Bucket:   "my-bucket",
					Endpoint: "s3.eu-central-1.amazonaws.com",
					Region:   "eu-central-1",
					Prefix:   "{{ .Profile.Name }}",
					ObjectLock: config.ObjectLockConfig{
						Mode:                "compliance",
						RetentionPeriod:     "720h",
						ExtendOnMaintenance: yes,
					},
				},
				Password: config.Password{
					Source:         "keyring",
					KeyringService: "kopiaprofile",
				},
				Env: map[string]string{
					"AWS_REGION": "{{ .Env.AWS_REGION }}",
				},
				Backup: config.BackupSection{
					Sources: []string{"/home/user"},
					Tags:    []string{"nightly"},
				},
				Retention: config.RetentionSection{
					KeepLatest:  5,
					KeepDaily:   7,
					KeepWeekly:  4,
					KeepMonthly: 6,
					KeepAnnual:  2,
				},
				Verify: config.VerifySection{
					FilesPercent: 1.0,
				},
				Lock: config.LockSection{
					Path: "~/.cache/kopiaprofile/{{ .Profile.Name }}.lock",
				},
			},
			"home": {
				Inherit:     "default-base",
				Description: "Nightly backup of /home/user",
				Backup: config.BackupSection{
					Sources: []string{"/home/user"},
					Tags:    []string{"home", "host:{{ .Hostname }}"},
				},
				Retention: config.RetentionSection{
					KeepLatest: 3,
				},
			},
		},
	}
}

// newInitCmd returns the `init` subcommand.
func newInitCmd(flags *rootFlags) *cobra.Command {
	var force bool
	var format string
	cmd := &cobra.Command{
		Use:   "init <config-file>",
		Short: "Generate a skeleton configuration file",
		Long: `init writes a working example configuration to the given path.
The output format is auto-detected from the file extension (.yaml, .yml,
.toml, .hcl, .json) and can be overridden with --format.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := args[0]
			if _, err := osStat(path); err == nil && !force {
				return fmt.Errorf("file %q already exists; use --force to overwrite", path)
			}
			// Determine format: explicit flag wins, else extension.
			chosen := strings.ToLower(format)
			if chosen == "" {
				chosen = config.DetectFormat(path)
			}
			// HCL output is not implemented (HCL is a write-once language).
			if chosen == "hcl" {
				return fmt.Errorf("writing HCL is not supported; use yaml, toml, or json")
			}
			data, err := skeletonConfig().Marshal(chosen)
			if err != nil {
				return err
			}
			return writeString(path, string(data))
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing config file")
	cmd.Flags().StringVar(&format, "format", "", "config format (yaml|toml|json); auto-detected from extension if empty")
	return cmd
}
