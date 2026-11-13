package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/hashicorp/go-version"
	"github.com/suzuki-shunsuke/slog-error/slogerr"
)

func (c *Controller) Run(_ context.Context, _ *slog.Logger, param *ParamRun, vout *TerraformVersionOutput) error {
	cfg := &Config{}
	if err := c.readConfig(cfg, param); err != nil {
		return err
	}

	policies, err := parseConfig(cfg)
	if err != nil {
		return err
	}

	return validate(vout, policies)
}

// providerPolicy is the merged policy of a provider.
// allow is the intersection of version constraints of duplicated allow list entries.
// nil allow means any version is allowed.
// deny is the list of version constraints of deny list entries.
// The provider is denied if any of them matches. A nil constraint denies any version.
type providerPolicy struct {
	allow version.Constraints
	deny  []version.Constraints
}

func parseConfig(cfg *Config) (map[string]*providerPolicy, error) {
	policies := make(map[string]*providerPolicy, len(cfg.Providers)+len(cfg.DeniedProviders))
	for _, provider := range cfg.Providers {
		constraints, err := parseProviderConstraints(provider)
		if err != nil {
			return nil, err
		}
		policy := getPolicy(policies, provider.Name)
		policy.allow = intersectConstraints(policy.allow, constraints)
	}
	for _, provider := range cfg.DeniedProviders {
		constraints, err := parseProviderConstraints(provider)
		if err != nil {
			return nil, err
		}
		policy := getPolicy(policies, provider.Name)
		policy.deny = append(policy.deny, constraints)
	}
	return policies, nil
}

func getPolicy(policies map[string]*providerPolicy, name string) *providerPolicy {
	policy, ok := policies[name]
	if !ok {
		policy = &providerPolicy{}
		policies[name] = policy
	}
	return policy
}

func parseProviderConstraints(provider *Provider) (version.Constraints, error) {
	if provider.Name == "" {
		return nil, ErrProviderNameIsRequired
	}
	if provider.VersionConstraints == "" {
		return nil, nil
	}
	constraints, err := version.NewConstraint(provider.VersionConstraints)
	if err != nil {
		return nil, fmt.Errorf("parse version constraints: %w", slogerr.With(err,
			"provider_name", provider.Name,
			"provider_version_constraints", provider.VersionConstraints,
		))
	}
	return constraints, nil
}

// intersectConstraints returns the intersection of two constraints.
// nil constraints means no restriction, so the intersection is the other side.
func intersectConstraints(a, b version.Constraints) version.Constraints {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	merged := make(version.Constraints, 0, len(a)+len(b))
	merged = append(merged, a...)
	return append(merged, b...)
}

func validate(vout *TerraformVersionOutput, policies map[string]*providerPolicy) error {
	names := make([]string, 0, len(vout.ProviderSelections))
	for name := range vout.ProviderSelections {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []error
	for _, providerName := range names {
		providerVersion := vout.ProviderSelections[providerName]
		v, err := version.NewVersion(providerVersion)
		if err != nil {
			// Fail closed if the version can't be parsed
			return fmt.Errorf("parse the provider version as semver: %w", slogerr.With(err,
				"provider_name", providerName,
				"provider_version", providerVersion,
			))
		}
		policy, ok := policies[providerName]
		if !ok {
			errs = append(errs, slogerr.With(ErrDisallowedProvider, "provider_name", providerName)) //nolint:wrapcheck
			continue
		}
		if denied := checkDeny(policy, providerName, providerVersion, v); denied != nil {
			errs = append(errs, denied)
			continue
		}
		if !policy.allow.Check(v) {
			errs = append(errs, slogerr.With(ErrDisallowedProviderVersion, //nolint:wrapcheck
				"provider_name", providerName,
				"provider_version", providerVersion,
				"provider_version_constraints", policy.allow.String(),
			))
		}
	}
	return errors.Join(errs...)
}

// checkDeny returns an error if the provider is denied.
// The deny list takes precedence over the allow list.
func checkDeny(policy *providerPolicy, providerName, providerVersion string, v *version.Version) error {
	for _, constraints := range policy.deny {
		if !constraints.Check(v) {
			continue
		}
		if len(constraints) == 0 {
			return slogerr.With(ErrDeniedProvider, //nolint:wrapcheck
				"provider_name", providerName,
				"provider_version", providerVersion,
			)
		}
		return slogerr.With(ErrDeniedProvider, //nolint:wrapcheck
			"provider_name", providerName,
			"provider_version", providerVersion,
			"provider_version_constraints", constraints.String(),
		)
	}
	return nil
}
