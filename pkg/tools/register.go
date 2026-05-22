package tools

import (
	"fmt"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	anicmd "github.com/chainreactors/aiscan/pkg/tools/ani"
	cyberhubcmd "github.com/chainreactors/aiscan/pkg/tools/cyberhub"
	gogocmd "github.com/chainreactors/aiscan/pkg/tools/gogo"
	inacmd "github.com/chainreactors/aiscan/pkg/tools/ina"
	katanacmd "github.com/chainreactors/aiscan/pkg/tools/katana"
	neutroncmd "github.com/chainreactors/aiscan/pkg/tools/neutron"
	"github.com/chainreactors/aiscan/pkg/tools/scan"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	spraycmd "github.com/chainreactors/aiscan/pkg/tools/spray"
	zombiecmd "github.com/chainreactors/aiscan/pkg/tools/zombie"
)

func RegisterAll(reg *ScannerRegistry, engineSet *engine.Set, opts ...scan.Option) error {
	return RegisterAllWithLogger(reg, engineSet, telemetry.NopLogger(), opts...)
}

func RegisterAllWithLogger(reg *ScannerRegistry, engineSet *engine.Set, logger telemetry.Logger, opts ...scan.Option) error {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	if engineSet == nil {
		engineSet = &engine.Set{}
	}
	scanOpts := append([]scan.Option{scan.WithLogger(logger)}, opts...)
	reg.Register(katanacmd.New())
	if engineSet.Ani != nil {
		reg.Register(anicmd.New(engineSet.Ani).WithLogger(logger).WithDefaults(anicmd.Defaults{
			Depth:      engineSet.Recon.AniDepth,
			DepthSet:   engineSet.Recon.AniDepthSet,
			Percent:    engineSet.Recon.AniPercent,
			PercentSet: engineSet.Recon.AniPercentSet,
			Proxy:      engineSet.Recon.AniProxy,
			TycToken:   engineSet.Recon.AniTycToken,
			QccCookie:  engineSet.Recon.AniQccCookie,
			AqcCookie:  engineSet.Recon.AniAqcCookie,
		}))
	}
	if engineSet.Ina != nil {
		reg.Register(inacmd.New(engineSet.Ina).WithLogger(logger))
	}
	if engineSet.Resources != nil {
		reg.Register(cyberhubcmd.New(engineSet.Resources))
	}
	if engineSet.Gogo != nil && engineSet.Spray != nil {
		reg.Register(scan.New(engineSet, scanOpts...))
	}
	if engineSet.Gogo != nil {
		reg.Register(gogocmd.New(engineSet.Gogo).WithLogger(logger))
	}
	if engineSet.Spray != nil {
		reg.Register(spraycmd.New(engineSet.Spray).WithLogger(logger))
	}
	if engineSet.Zombie != nil {
		reg.Register(zombiecmd.New(engineSet.Zombie).WithLogger(logger))
	}
	if engineSet.Neutron != nil {
		reg.Register(neutroncmd.New(engineSet.Neutron, engineSet.Index).WithLogger(logger))
	}

	logger.Infof("scanner commands=%s", fmt.Sprintf("%v", reg.Names()))
	return nil
}
