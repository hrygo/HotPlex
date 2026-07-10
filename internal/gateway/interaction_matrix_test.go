package gateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

type matrixWorker struct {
	*mockWorkerForHandler
	workerType worker.WorkerType
}

func (w *matrixWorker) Type() worker.WorkerType { return w.workerType }

type matrixMetadataRecorder struct {
	kind       string
	requestID  string
	answerList map[string][]string
}

func (r *matrixMetadataRecorder) HandlePermissionResponse(_ context.Context, requestID string, _ bool, _ string) error {
	r.kind = "permission"
	r.requestID = requestID
	return nil
}

func (r *matrixMetadataRecorder) HandleQuestionResponse(_ context.Context, requestID string, _ map[string]string) error {
	r.kind = "question-basic"
	r.requestID = requestID
	return nil
}

func (r *matrixMetadataRecorder) HandleQuestionResponseOptions(_ context.Context, requestID string, answers map[string][]string, _ []string) error {
	r.kind = "question"
	r.requestID = requestID
	r.answerList = answers
	return nil
}

func (r *matrixMetadataRecorder) HandleElicitationResponse(_ context.Context, requestID, _ string, _ map[string]any) error {
	r.kind = "elicitation"
	r.requestID = requestID
	return nil
}

func TestInteractionE2EMatrix_AllPlatformsWorkersAndKinds(t *testing.T) {
	t.Parallel()

	platforms := []string{"feishu", "slack", "webchat"}
	workers := []worker.WorkerType{
		worker.TypeOpenCodeSrv,
		worker.TypeClaudeCode,
		worker.TypeCodexCLI,
		worker.TypeACP,
	}
	interactions := []string{"permission", "question", "elicitation"}

	for _, platform := range platforms {
		for _, workerType := range workers {
			for _, interaction := range interactions {
				name := fmt.Sprintf("%s/%s/%s", platform, workerType, interaction)
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					requestID := fmt.Sprintf("%s-%s-%s", platform, workerType, interaction)
					recorder := &matrixMetadataRecorder{}
					baseWorker := new(mockWorkerForHandler)
					w := &matrixWorker{mockWorkerForHandler: baseWorker, workerType: workerType}
					baseWorker.On("Input", mock.Anything, "", mock.Anything).Run(func(args mock.Arguments) {
						metadata, ok := args.Get(2).(map[string]any)
						require.True(t, ok)
						handled, err := base.DispatchMetadata(args.Get(0).(context.Context), metadata, recorder)
						require.NoError(t, err)
						require.True(t, handled)
					}).Return(nil)

					sm := new(mockInputSM)
					sessionID := "session-" + requestID
					sm.On("GetWorker", sessionID).Return(w)
					h := newInputHandler(t, sm)

					if platform == "webchat" {
						sm.On("Get", sessionID).Return(&session.SessionInfo{Platform: platform}, nil)
						env := explicitMatrixResponse(sessionID, requestID, interaction)
						require.NoError(t, h.handleInteractionResponseEvent(context.Background(), env))
					} else {
						metadata := matrixResponseMetadata(requestID, interaction)
						env := inputEnvelopeWithMetadata(sessionID, "", metadata)
						require.NoError(t, h.handleInput(context.Background(), env))
					}

					require.Equal(t, interaction, recorder.kind)
					require.Equal(t, requestID, recorder.requestID)
					if interaction == "question" {
						require.Equal(t, []string{"Unit", "Race"}, recorder.answerList["checks"])
					}
					sm.AssertExpectations(t)
					baseWorker.AssertExpectations(t)
				})
			}
		}
	}
}

func matrixResponseMetadata(requestID, interaction string) map[string]any {
	switch interaction {
	case "permission":
		return messaging.BuildPermissionResponse(requestID, true, "approved in matrix")
	case "question":
		return messaging.BuildQuestionResponseOptionsWithOrder(requestID, map[string][]string{
			"checks": {"Unit", "Race"},
		}, []string{"checks"})
	case "elicitation":
		return map[string]any{
			"elicitation_response": map[string]any{
				"id":      requestID,
				"action":  "accept",
				"content": map[string]any{"root": "/tmp"},
			},
		}
	default:
		panic("unknown interaction " + interaction)
	}
}

func explicitMatrixResponse(sessionID, requestID, interaction string) *events.Envelope {
	var kind events.Kind
	var data map[string]any
	switch interaction {
	case "permission":
		kind = events.PermissionResponse
		data = map[string]any{"id": requestID, "allowed": true, "reason": "approved in matrix"}
	case "question":
		kind = events.QuestionResponse
		data = map[string]any{"id": requestID, "answers": map[string]any{"checks": []any{"Unit", "Race"}}}
	case "elicitation":
		kind = events.ElicitationResponse
		data = map[string]any{"id": requestID, "action": "accept", "content": map[string]any{"root": "/tmp"}}
	default:
		panic("unknown interaction " + interaction)
	}
	return events.NewEnvelope("matrix-response", sessionID, 1, kind, data)
}
