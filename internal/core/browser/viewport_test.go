package browser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewport_NormalizedFillsTheScale(t *testing.T) {
	t.Parallel()

	got := Viewport{Width: 2500, Height: 1600}.Normalized()
	assert.Equal(t, 1.0, got.Scale)
}

func TestViewport_ValidateRejectsUnusableSizes(t *testing.T) {
	t.Parallel()

	assert.Error(t, Viewport{}.Validate(), "a viewport without a size is not a viewport")
	assert.Error(t, Viewport{Width: 2500, Height: 1600, Scale: 100}.Validate(), "the scale is bounded")
	assert.NoError(t, Viewport{Width: 2500, Height: 1600}.Normalized().Validate())
}

func TestDesignCanvasIsThe2500x1600Artboard(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2500, DesignCanvas().Width)
	assert.Equal(t, 1600, DesignCanvas().Height)
}

func TestSetViewport_SendsDeviceMetricsOverride(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.SetViewport(context.Background(), Viewport{Width: 2500, Height: 1600}))

	msg, ok := f.find("Emulation.setDeviceMetricsOverride")
	require.True(t, ok)
	assert.JSONEq(t, `{"width":2500,"height":1600,"deviceScaleFactor":1,"mobile":false,"screenWidth":2500,"screenHeight":1600}`, string(msg.Params))
	assert.Equal(t, "2500x1600@1x", s.CurrentViewport().String())
}

func TestSetViewport_RejectsAnInvalidSizeBeforeTalkingToTheBrowser(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.SetViewport(context.Background(), Viewport{Width: 0, Height: 100})
	require.Error(t, err)
	_, ok := f.find("Emulation.setDeviceMetricsOverride")
	assert.False(t, ok, "an invalid viewport must not reach the browser")
}

func TestClearViewport_ForgetsTheOverride(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers()

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.SetViewport(context.Background(), Viewport{Width: 2500, Height: 1600}))
	require.NoError(t, s.ClearViewport(context.Background()))

	_, ok := f.find("Emulation.clearDeviceMetricsOverride")
	require.True(t, ok)
	assert.Equal(t, Viewport{}, s.CurrentViewport())
}
