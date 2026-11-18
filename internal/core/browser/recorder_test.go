package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartRecording_CapturesFramesAndAcknowledgesThem(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	rec, err := s.StartRecording(context.Background(), RecordOptions{FPS: 10, Quality: 50})
	require.NoError(t, err)

	msg, ok := f.find("Page.startScreencast")
	require.True(t, ok)
	assert.JSONEq(t, `{"format":"jpeg","quality":50,"everyNthFrame":1}`, string(msg.Params))

	pushFrame := func(data string, ts float64, sessionID int64) {
		f.push(fakeSessionID, "Page.screencastFrame", map[string]any{
			"data":     base64.StdEncoding.EncodeToString([]byte(data)),
			"metadata": map[string]any{"timestamp": ts, "sessionId": sessionID},
		})
	}
	pushFrame("frame-1", 1, 7)
	pushFrame("frame-2", 2, 7)

	require.Eventually(t, func() bool { return rec.FrameCount() == 2 }, 2*time.Second, 10*time.Millisecond,
		"the screencast frames should arrive")

	// The browser only sends the next frame once the previous one is
	// acknowledged, so the acknowledgement is not optional.
	require.Eventually(t, func() bool {
		_, found := f.find("Page.screencastFrameAck")
		return found
	}, 2*time.Second, 10*time.Millisecond, "each frame must be acknowledged")

	ack, _ := f.find("Page.screencastFrameAck")
	assert.JSONEq(t, `{"sessionId":7}`, string(ack.Params))

	frames, err := rec.Stop(context.Background())
	require.NoError(t, err)
	require.Len(t, frames, 2)
	assert.Equal(t, "frame-1", string(frames[0].Data))
	assert.Equal(t, time.Duration(1)*time.Second, frames[0].Timestamp)
	assert.Equal(t, time.Duration(2)*time.Second, frames[1].Timestamp)
	assert.False(t, rec.Recording())
	_, ok = f.find("Page.stopScreencast")
	assert.True(t, ok)
}

func TestStopRecording_WithoutARecordingIsAnError(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	rec, err := s.StartRecording(context.Background(), RecordOptions{})
	require.NoError(t, err)
	_, err = rec.Stop(context.Background())
	require.NoError(t, err)

	_, err = rec.Stop(context.Background())
	assert.Error(t, err, "stopping twice must not pretend to work")
}

func TestRecordOptions_NormalizedFillsTheDefaults(t *testing.T) {
	t.Parallel()

	got := RecordOptions{}.normalized()
	assert.Equal(t, defaultRecordFPS, got.FPS)
	assert.Equal(t, defaultRecordQuality, got.Quality)
	assert.Equal(t, 1, got.EveryNthFrame)
}

func TestStartRecording_ScalesTheCapturedFrames(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	_, err = s.StartRecording(context.Background(), RecordOptions{MaxWidth: 1280, MaxHeight: 720})
	require.NoError(t, err)

	msg, ok := f.find("Page.startScreencast")
	require.True(t, ok)
	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Equal(t, float64(1280), params["maxWidth"])
	assert.Equal(t, float64(720), params["maxHeight"])
}
