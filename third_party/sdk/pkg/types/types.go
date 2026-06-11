package types

import (
	"context"
	"io"

	fingersLib "github.com/chainreactors/fingers"
	"github.com/chainreactors/fingers/alias"
	"github.com/chainreactors/fingers/common"
	fingersEngine "github.com/chainreactors/fingers/fingers"
	gogopkg "github.com/chainreactors/gogo/v2/pkg"
	"github.com/chainreactors/neutron/operators"
	zombiecore "github.com/chainreactors/zombie/core"
	"github.com/chainreactors/neutron/protocols"
	templateHTTP "github.com/chainreactors/neutron/protocols/http"
	templateNetwork "github.com/chainreactors/neutron/protocols/network"
	"github.com/chainreactors/neutron/templates"
	"github.com/chainreactors/parsers"
)

// ====================
// SDK Core
// ====================

type (
	Engine interface {
		Name() string
		Execute(ctx Context, task Task) (<-chan Result, error)
		io.Closer
	}

	// Provider 是统一的数据源接口，CyberHub 和 Embed 均实现此接口。
	Provider interface {
		Fingers(ctx context.Context) (Fingers, []*Alias, error)
		POCs(ctx context.Context) ([]*Template, error)
	}

	Context interface {
		Context() context.Context
	}

	Config interface {
		Validate() error
	}

	Task interface {
		Type() string
		Validate() error
	}

	Result interface {
		Success() bool
		Error() error
		Data() interface{}
	}
)

// ====================
// Scan Results
// ====================

type (
	GOGOResult    = parsers.GOGOResult
	GOGOResults   = parsers.GOGOResults
	GOGOConfig    = parsers.GOGOConfig
	GOGOData      = parsers.GOGOData
	SprayResult   = parsers.SprayResult
	SpraySource   = parsers.SpraySource
	ZombieResult  = parsers.ZombieResult
	ZombieInput   = parsers.ZombieInput
	ZombieTaskMod = parsers.ZombieTaskMod
	Extracted     = parsers.Extracted
	Extracteds    = parsers.Extracteds
	Extractor     = parsers.Extractor
	Extractors    = parsers.Extractors
	Response      = parsers.Response
	Content       = parsers.Content
	Hashes        = parsers.Hashes
)

const (
	CheckSource      = parsers.CheckSource
	InitRandomSource = parsers.InitRandomSource
	InitIndexSource  = parsers.InitIndexSource
	RedirectSource   = parsers.RedirectSource
	CrawlSource      = parsers.CrawlSource
	FingerSource     = parsers.FingerSource
	WordSource       = parsers.WordSource
	WafSource        = parsers.WafSource
	RuleSource       = parsers.RuleSource
	BakSource        = parsers.BakSource
	CommonFileSource = parsers.CommonFileSource
	UpgradeSource    = parsers.UpgradeSource
	RetrySource      = parsers.RetrySource
	AppendSource     = parsers.AppendSource
	AppendRuleSource = parsers.AppendRuleSource

	ZombieModBrute     = parsers.ZombieModBrute
	ZombieModUnauth    = parsers.ZombieModUnauth
	ZombieModCheck     = parsers.ZombieModCheck
	ZombieModSniper    = parsers.ZombieModSniper
	ZombieModPitchfork = parsers.ZombieModPitchfork
)

// ====================
// Fingerprint
// ====================

type (
	Aliases       = alias.Aliases
	Alias         = alias.Alias
	Framework     = common.Framework
	Frameworks    = common.Frameworks
	Vuln          = common.Vuln
	Vulns         = common.Vulns
	Attributes    = common.Attributes
	ServiceResult = common.ServiceResult
	MatchDetail   = common.MatchDetail
	From          = common.From

	FingerprintType  = common.FingerprintType
	EngineCapability = common.EngineCapability
)

const (
	WebFingerprint     = common.WebFingerprint
	ServiceFingerprint = common.ServiceFingerprint

	FrameFromDefault        = common.FrameFromDefault
	FrameFromACTIVE         = common.FrameFromACTIVE
	FrameFromICO            = common.FrameFromICO
	FrameFromNOTFOUND       = common.FrameFromNOTFOUND
	FrameFromGUESS          = common.FrameFromGUESS
	FrameFromRedirect       = common.FrameFromRedirect
	FrameFromFingers        = common.FrameFromFingers
	FrameFromFingerprintHub = common.FrameFromFingerprintHub
	FrameFromWappalyzer     = common.FrameFromWappalyzer
	FrameFromEhole          = common.FrameFromEhole
	FrameFromGoby           = common.FrameFromGoby
	FrameFromNmap           = common.FrameFromNmap
)

// ====================
// Fingers Library
// ====================

type (
	Finger             = fingersEngine.Finger
	Fingers            = fingersEngine.Fingers
	FingerMapper       = fingersEngine.FingerMapper
	FingerRule         = fingersEngine.Rule
	FingerRules        = fingersEngine.Rules
	FingerRegexps      = fingersEngine.Regexps
	FingerFavicons     = fingersEngine.Favicons
	FingerContent      = fingersEngine.Content
	FingerSender       = fingersEngine.Sender
	FingerCallback     = fingersEngine.Callback
	FingerRegexp       = fingersEngine.CompiledRegexp
	FingersMatchEngine = fingersEngine.FingersEngine
	FingersLibEngine   = fingersLib.Engine
)

// ====================
// Template
// ====================

type (
	Template               = templates.Template
	TemplateInfo           = templates.Info
	Classification         = templates.Classification
	TemplateVariable       = protocols.Variable
	TemplateRequest        = protocols.Request
	HTTPTemplateRequest    = templateHTTP.Request
	NetworkTemplateRequest = templateNetwork.Request
)

// ====================
// Neutron Execution
// ====================

type (
	OperatorResult    = operators.Result
	Operators         = operators.Operators
	Matcher           = operators.Matcher
	MatcherType       = operators.MatcherType
	ConditionType     = operators.ConditionType
	OperatorExtractor = operators.Extractor
	ExtractorType     = operators.ExtractorType
	ResultEvent       = protocols.ResultEvent
)

var (
	OpsecError = protocols.OpsecError
)

const (
	RegexExtractor = operators.RegexExtractor
	KValExtractor  = operators.KValExtractor
	JSONExtractor  = operators.JSONExtractor
	DSLExtractor   = operators.DSLExtractor

	WordsMatcher   = operators.WordsMatcher
	RegexMatcher   = operators.RegexMatcher
	BinaryMatcher  = operators.BinaryMatcher
	StatusMatcher  = operators.StatusMatcher
	SizeMatcher    = operators.SizeMatcher
	DSLMatcher     = operators.DSLMatcher
	FaviconMatcher = operators.FaviconMatcher

	ANDCondition = operators.ANDCondition
	ORCondition  = operators.ORCondition
)

// ====================
// Gogo
// ====================

type (
	Workflow   = gogopkg.Workflow
	GogoOption = gogopkg.RunnerOption
)

// ====================
// Zombie
// ====================

type (
	ZombieOption = zombiecore.RunnerOption
	ZombieTarget = zombiecore.Target
)

var (
	ZombieModeBomb      = zombiecore.ModBomb
	ZombieModePitchFork = zombiecore.ModPitchFork
	ZombieModeSniper    = zombiecore.ModSniper
)
