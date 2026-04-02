// Package aep provides the gateway's AEP v1 codec implementation.
// This package re-exports from pkg/aep for backward compatibility.
// Gateway-specific codec logic should be added here.
package aep

import (
	"hotplex-worker/pkg/aep"
)

// Re-export types and functions from pkg/aep for backward compatibility.
type (
	WorkerType = aep.WorkerType
	InitData      = aep.InitData
	InitAuth      = aep.InitAuth
	InitConfig    = aep.InitConfig
	ClientCaps    = aep.ClientCaps
	InitAckData   = aep.InitAckData
	ServerCaps    = aep.ServerCaps
	InitError     = aep.InitError
)

const (
	Init    = aep.Init
	InitAck = aep.InitAck
)

var (
	WorkerClaudeCode  = aep.WorkerClaudeCode
	WorkerOpenCodeCLI = aep.WorkerOpenCodeCLI
	WorkerOpenCodeSrv = aep.WorkerOpenCodeSrv
	WorkerPiMono      = aep.WorkerPiMono
)

// Re-export functions from pkg/aep.
var (
	Encode           = aep.Encode
	EncodeChunk      = aep.EncodeChunk
	Decode           = aep.Decode
	DecodeLine       = aep.DecodeLine
	Validate         = aep.Validate
	ValidateMinimal   = aep.ValidateMinimal
	NewID            = aep.NewID
	NewSessionID     = aep.NewSessionID
	EncodeJSON       = aep.EncodeJSON
	MustMarshal      = aep.MustMarshal
	IsSessionBusy    = aep.IsSessionBusy
	IsTerminalEvent  = aep.IsTerminalEvent
	ParseSessionID   = aep.ParseSessionID
	NewEnvelope      = aep.NewEnvelope
	NewInputEnvelope = aep.NewInputEnvelope
	NewPingEnvelope  = aep.NewPingEnvelope
	NewInitEnvelope  = aep.NewInitEnvelope
	BuildInitAck     = aep.BuildInitAck
	BuildInitAckError = aep.BuildInitAckError
	ValidateInit     = aep.ValidateInit
	DefaultServerCaps = aep.DefaultServerCaps
	BackoffDuration  = aep.BackoffDuration
)

// Re-export error types.
var (
	ErrInitVersionMismatch  = aep.ErrInitVersionMismatch
	ErrInitCapacityExceeded = aep.ErrInitCapacityExceeded
	ErrInitSessionNotFound  = aep.ErrInitSessionNotFound
	ErrInitSessionDeleted   = aep.ErrInitSessionDeleted
)

// WithSessionID is a functional option for NewInitEnvelope.
var WithSessionID = aep.WithSessionID

// WithAuthToken is a functional option for NewInitEnvelope.
var WithAuthToken = aep.WithAuthToken

// WithConfig is a functional option for NewInitEnvelope.
var WithConfig = aep.WithConfig

// SeqKey is exported for gateway use.
var SeqKey = aep.SeqKey

// escapeJSTerminators is re-exported for internal tests.
var escapeJSTerminators = aep.EscapeJSTerminators
