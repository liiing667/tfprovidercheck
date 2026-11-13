package controller_test

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/suzuki-shunsuke/tfprovidercheck/pkg/controller"
)

const (
	dirPermission  os.FileMode = 0o775
	filePermission os.FileMode = 0o644

	workspacePath     = "/workspace"
	workspaceFooYAML  = workspacePath + "/foo.yaml"
	awsProvider       = "registry.terraform.io/hashicorp/aws"
	awsVersion        = "3.0.0"
	awsProviderConfig = `providers:
- name: registry.terraform.io/hashicorp/aws
`
)

func newFs(files map[string]string, dirs ...string) (afero.Fs, error) {
	fs := afero.NewMemMapFs()
	for name, body := range files {
		if err := afero.WriteFile(fs, name, []byte(body), filePermission); err != nil {
			return nil, err //nolint:wrapcheck
		}
	}
	for _, dir := range dirs {
		if err := fs.MkdirAll(dir, dirPermission); err != nil {
			return nil, err //nolint:wrapcheck
		}
	}
	return fs, nil
}

func TestController_Run(t *testing.T) { //nolint:gocognit,cyclop,funlen
	t.Parallel()
	data := []struct {
		name   string
		files  map[string]string
		dirs   []string
		param  *controller.ParamRun
		vout   *controller.TerraformVersionOutput
		isErr  bool
		err    error
		errMsg string
	}{
		{
			name: "invalid config body",
			param: &controller.ParamRun{
				ConfigBody: "{",
			},
			errMsg: `marshal configuration as YAML: `,
		},
		{
			name:   "config isn't found",
			param:  &controller.ParamRun{},
			errMsg: `open a configuration file: `,
		},
		{
			name: "config path",
			param: &controller.ParamRun{
				ConfigFilePath: workspaceFooYAML,
				PWD:            workspacePath,
			},
			files: map[string]string{
				workspaceFooYAML: awsProviderConfig,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
		},
		{
			name: "invalid config file",
			param: &controller.ParamRun{
				ConfigFilePath: workspaceFooYAML,
				PWD:            workspacePath,
			},
			files: map[string]string{
				workspaceFooYAML: `{`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			errMsg: `read a configuration file as YAML: `,
		},
		{
			name: "invalid config file path",
			param: &controller.ParamRun{
				ConfigFilePath: workspaceFooYAML,
				PWD:            workspacePath,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			errMsg: `open a configuration file: `,
		},
		{
			name: "config file on the current directory",
			param: &controller.ParamRun{
				PWD: workspacePath,
			},
			files: map[string]string{
				".tfprovidercheck.yaml": awsProviderConfig,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
		},
		{
			name: "config body",
			param: &controller.ParamRun{
				ConfigBody: awsProviderConfig,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
		},
		{
			name: "provider name is required",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- version: ">= 3.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			err: controller.ErrProviderNameIsRequired,
		},
		{
			name: "invalid version constraint",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
  version: "=> 3.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			errMsg: "parse version constraints",
		},
		{
			name: "disallowed provider",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: foo
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			err: controller.ErrDisallowedProvider,
		},
		{
			name: "disallowed provider version",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
  version: ">= 4.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			err: controller.ErrDisallowedProviderVersion,
		},
		{
			name: "invalid version",
			param: &controller.ParamRun{
				ConfigBody: awsProviderConfig,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: "...",
				},
			},
			errMsg: `parse the provider version as semver: `,
		},
		{
			name: "denied provider overrides allow list",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
denied_providers:
- name: registry.terraform.io/hashicorp/aws
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			err: controller.ErrDeniedProvider,
		},
		{
			name: "denied provider version",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
denied_providers:
- name: registry.terraform.io/hashicorp/aws
  version: ">= 4.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: "4.5.0",
				},
			},
			err: controller.ErrDeniedProvider,
		},
		{
			name: "denied provider version doesn't match",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
denied_providers:
- name: registry.terraform.io/hashicorp/aws
  version: ">= 4.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
		},
		{
			name: "duplicated providers are intersected",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
  version: ">= 3.0.0"
- name: registry.terraform.io/hashicorp/aws
  version: "< 3.5.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: "3.6.0",
				},
			},
			err: controller.ErrDisallowedProviderVersion,
		},
		{
			name: "duplicated providers with empty version are intersected",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
- name: registry.terraform.io/hashicorp/aws
  version: ">= 4.0.0"
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider: awsVersion,
				},
			},
			err: controller.ErrDisallowedProviderVersion,
		},
		{
			name: "all violations are reported in the stable order",
			param: &controller.ParamRun{
				ConfigBody: `providers:
- name: registry.terraform.io/hashicorp/aws
denied_providers:
- name: registry.terraform.io/hashicorp/aws
`,
			},
			vout: &controller.TerraformVersionOutput{
				ProviderSelections: map[string]string{
					awsProvider:                              awsVersion,
					"registry.terraform.io/hashicorp/google": "4.0.0",
					"registry.terraform.io/hashicorp/azure":  "3.0.0",
				},
			},
			err:    controller.ErrDeniedProvider,
			errMsg: "this Terraform Provider is explicitly denied\nthis Terraform Provider is disallowed\nthis Terraform Provider is disallowed",
		},
	}
	logger := slog.New(slog.DiscardHandler)
	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			fs, err := newFs(d.files, d.dirs...)
			if err != nil {
				t.Fatal(err)
			}
			ctrl := controller.New(fs)
			if err := ctrl.Run(t.Context(), logger, d.param, d.vout); err != nil { //nolint:nestif
				if !d.isErr && d.err == nil && d.errMsg == "" {
					t.Fatal(err)
				}
				if d.err != nil {
					if !errors.Is(err, d.err) {
						t.Fatalf("wanted %v, got %v", d.err, err)
					}
				}
				if d.errMsg != "" {
					errMsg := err.Error()
					if !strings.Contains(errMsg, d.errMsg) {
						t.Fatalf(`error message doesn't include expected message. message=%s, expected_message=%s`, errMsg, d.errMsg)
					}
				}
			}
			if d.isErr {
				t.Fatal("error must be returned")
			}
		})
	}
}
