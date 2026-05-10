package scan

import (
	"context"

	"github.com/chainreactors/aiscan/pkg/scanner/engines"
)

func (c *Command) buildCapabilities(flags flags, opts scanOptions, profile profile) []capability {
	if c.engines == nil {
		c.engines = &engines.Set{}
	}

	capabilities := make([]capability, 0, len(profile.Capabilities)+1)
	gogoBuilt := false
	sprayBuilt := false
	weakpassBuilt := false
	for _, spec := range defaultCapabilitySpecs {
		if !profile.Enabled(spec.Name) || !spec.Available(c.engines) {
			continue
		}
		capabilities = append(capabilities, spec.Build(c, flags, opts, profile))
		if spec.Name == capGogoPortscan {
			gogoBuilt = true
		}
		if isSprayCapability(spec.Name) {
			sprayBuilt = true
		}
		if spec.Name == capZombieWeakpass {
			weakpassBuilt = true
		}
	}
	if opts.hasDiscoveryOverrides() && !gogoBuilt {
		c.logger.Warnf("scan %s port ignored unavailable", capGogoPortscan)
	}
	if opts.hasWebOverrides() && !sprayBuilt {
		c.logger.Warnf("scan web_probe dict,rule,word,default-dict,advance ignored unavailable")
	}
	if opts.hasWeakpassOverrides() && !weakpassBuilt {
		c.logger.Warnf("scan %s user,pwd ignored unavailable", capZombieWeakpass)
	}
	if verificationEnabled(flags.Verify) {
		if cap, ok := c.agentVerifyCapability(flags); ok {
			capabilities = append(capabilities, cap)
		}
	}
	return capabilities
}

type capabilitySpec struct {
	Name      string
	Available func(*engines.Set) bool
	Build     func(*Command, flags, scanOptions, profile) capability
}

var defaultCapabilitySpecs = []capabilitySpec{
	{
		Name:      capGogoPortscan,
		Available: hasGogo,
		Build: func(c *Command, _ flags, opts scanOptions, profile profile) capability {
			return capability{
				Name:   capGogoPortscan,
				Accept: acceptsTarget(targetScan),
				Worker: 4,
				Run: func(ctx context.Context, e event, emit emitFunc) {
					c.runPortDiscoveryCapability(ctx, opts.Discovery, profile, e.Target, emit)
				},
			}
		},
	},
	sprayCapabilitySpec(capSprayCheck, 4, sprayCheckOptions{}),
	sprayCapabilitySpec(capSprayFinger, 4, sprayCheckOptions{Finger: true}),
	{
		Name:      capCoreWeb,
		Available: alwaysAvailable,
		Build: func(_ *Command, _ flags, _ scanOptions, profile profile) capability {
			return capability{
				Name:   capCoreWeb,
				Accept: acceptsTarget(targetWebProbe),
				Worker: 2,
				Run: func(ctx context.Context, e event, emit emitFunc) {
					runWebResultAnalysisCapability(ctx, profile, e.Target, emit)
				},
			}
		},
	},
	sprayCapabilitySpec(capSprayCommon, 2, sprayCheckOptions{CommonPlugin: true}),
	sprayCapabilitySpec(capSprayBackup, 2, sprayCheckOptions{BakPlugin: true}),
	sprayCapabilitySpec(capSprayActive, 2, sprayCheckOptions{ActivePlugin: true, Finger: true}),
	{
		Name:      capSprayCrawl,
		Available: hasSpray,
		Build: func(c *Command, flags flags, opts scanOptions, profile profile) capability {
			return sprayCapability(flags, opts.Web, capSprayCrawl, 2, sprayCheckOptions{Crawl: true, CrawlDepth: profile.CrawlDepth}, c.runSprayCapability)
		},
	},
	sprayCapabilitySpec(capSprayBrute, 2, sprayCheckOptions{DefaultDict: true}),
	{
		Name:      capZombieWeakpass,
		Available: hasZombie,
		Build: func(c *Command, flags flags, opts scanOptions, _ profile) capability {
			return capability{
				Name:   capZombieWeakpass,
				Accept: acceptsTarget(targetWeakpass),
				Worker: 4,
				Run: func(ctx context.Context, e event, emit emitFunc) {
					c.runWeakpassCapability(ctx, flags, opts.Credentials, e.Target, emit)
				},
			}
		},
	},
	{
		Name:      capNeutronPOC,
		Available: hasNeutron,
		Build: func(c *Command, flags flags, _ scanOptions, _ profile) capability {
			return capability{
				Name:   capNeutronPOC,
				Accept: acceptsTarget(targetPOC),
				Worker: 4,
				Run: func(ctx context.Context, e event, emit emitFunc) {
					c.runPOCCapability(ctx, flags, e.Target, emit)
				},
			}
		},
	},
}

func sprayCapabilitySpec(name string, workers int, opts sprayCheckOptions) capabilitySpec {
	return capabilitySpec{
		Name:      name,
		Available: hasSpray,
		Build: func(c *Command, flags flags, scanOpts scanOptions, _ profile) capability {
			return sprayCapability(flags, scanOpts.Web, name, workers, opts, c.runSprayCapability)
		},
	}
}

func sprayCapability(flags flags, web webOptions, name string, workers int, opts sprayCheckOptions, run func(context.Context, flags, webOptions, target, string, sprayCheckOptions, emitFunc)) capability {
	return capability{
		Name:   name,
		Accept: acceptsTarget(targetWeb),
		Worker: workers,
		Run: func(ctx context.Context, e event, emit emitFunc) {
			run(ctx, flags, web, e.Target, name, opts, emit)
		},
	}
}

func isSprayCapability(name string) bool {
	switch name {
	case capSprayCheck, capSprayFinger, capSprayCommon, capSprayBackup, capSprayActive, capSprayCrawl, capSprayBrute:
		return true
	default:
		return false
	}
}

func alwaysAvailable(_ *engines.Set) bool {
	return true
}

func hasGogo(engineSet *engines.Set) bool {
	return engineSet != nil && engineSet.Gogo != nil
}

func hasSpray(engineSet *engines.Set) bool {
	return engineSet != nil && engineSet.Spray != nil
}

func hasZombie(engineSet *engines.Set) bool {
	return engineSet != nil && engineSet.Zombie != nil
}

func hasNeutron(engineSet *engines.Set) bool {
	return engineSet != nil && engineSet.Neutron != nil
}
