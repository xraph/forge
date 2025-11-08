package sdk

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

func TestMultiModalBuilder_WithText(t *testing.T) {
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithText("Hello, world!")

	if len(builder.contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(builder.contents))
	}

	if builder.contents[0].Type != ContentTypeText {
		t.Errorf("expected text content type, got %v", builder.contents[0].Type)
	}

	if builder.contents[0].Text != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %s", builder.contents[0].Text)
	}
}

func TestMultiModalBuilder_WithImage(t *testing.T) {
	imageData := []byte("fake image data")
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithImage(imageData, "image/png")

	if len(builder.contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(builder.contents))
	}

	if builder.contents[0].Type != ContentTypeImage {
		t.Errorf("expected image content type, got %v", builder.contents[0].Type)
	}

	if string(builder.contents[0].Data) != string(imageData) {
		t.Errorf("expected image data to match")
	}

	if builder.contents[0].MimeType != "image/png" {
		t.Errorf("expected mime type 'image/png', got %s", builder.contents[0].MimeType)
	}
}

func TestMultiModalBuilder_WithImageURL(t *testing.T) {
	url := "https://example.com/image.jpg"
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithImageURL(url)

	if len(builder.contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(builder.contents))
	}

	if builder.contents[0].Type != ContentTypeImage {
		t.Errorf("expected image content type, got %v", builder.contents[0].Type)
	}

	if builder.contents[0].URL != url {
		t.Errorf("expected URL %s, got %s", url, builder.contents[0].URL)
	}
}

func TestMultiModalBuilder_WithAudio(t *testing.T) {
	audioData := []byte("fake audio data")
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithAudio(audioData, "audio/mpeg")

	if len(builder.contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(builder.contents))
	}

	if builder.contents[0].Type != ContentTypeAudio {
		t.Errorf("expected audio content type, got %v", builder.contents[0].Type)
	}

	if string(builder.contents[0].Data) != string(audioData) {
		t.Errorf("expected audio data to match")
	}
}

func TestMultiModalBuilder_WithVideo(t *testing.T) {
	videoData := []byte("fake video data")
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithVideo(videoData, "video/mp4")

	if len(builder.contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(builder.contents))
	}

	if builder.contents[0].Type != ContentTypeVideo {
		t.Errorf("expected video content type, got %v", builder.contents[0].Type)
	}

	if string(builder.contents[0].Data) != string(videoData) {
		t.Errorf("expected video data to match")
	}
}

func TestMultiModalBuilder_BuildPrompt(t *testing.T) {
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).
		WithText("What's in this image?").
		WithImageURL("https://example.com/image.jpg").
		WithText("Please provide details.")

	prompt, err := builder.buildPrompt()
	if err != nil {
		t.Fatalf("buildPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "What's in this image?") {
		t.Errorf("prompt should contain text content")
	}

	if !strings.Contains(prompt, "https://example.com/image.jpg") {
		t.Errorf("prompt should contain image URL")
	}

	if !strings.Contains(prompt, "Please provide details.") {
		t.Errorf("prompt should contain second text content")
	}
}

func TestMultiModalBuilder_BuildPrompt_WithImageData(t *testing.T) {
	imageData := []byte("fake image data")
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).
		WithText("Analyze this image:").
		WithImage(imageData, "image/png")

	prompt, err := builder.buildPrompt()
	if err != nil {
		t.Fatalf("buildPrompt failed: %v", err)
	}

	// Should contain base64 encoded data URL
	encoded := base64.StdEncoding.EncodeToString(imageData)
	if !strings.Contains(prompt, encoded) {
		t.Errorf("prompt should contain base64 encoded image")
	}

	if !strings.Contains(prompt, "data:image/png;base64") {
		t.Errorf("prompt should contain data URL prefix")
	}
}

func TestMultiModalBuilder_Execute(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "I see a beautiful landscape with mountains and a lake.",
					},
					FinishReason: "stop",
				},
			},
			Usage: &llm.LLMUsage{
				InputTokens:  100,
				OutputTokens: 50,
				TotalTokens:  150,
			},
		}, nil
	}

	builder := NewMultiModalBuilder(
		context.Background(),
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)

	result, err := builder.
		WithImageURL("https://example.com/landscape.jpg").
		WithText("Describe this image").
		Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if result.Text != "I see a beautiful landscape with mountains and a lake." {
		t.Errorf("unexpected text: %s", result.Text)
	}

	if result.Usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", result.Usage.InputTokens)
	}

	if result.Usage.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", result.Usage.OutputTokens)
	}
}

func TestMultiModalBuilder_Execute_NoContent(t *testing.T) {
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)

	_, err := builder.Execute()
	if err == nil {
		t.Error("expected error for no content, got nil")
	}

	if !strings.Contains(err.Error(), "no content provided") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestMultiModalBuilder_WithSystemMessage(t *testing.T) {
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).WithSystemMessage("You are a helpful assistant")

	if builder.systemMsg != "You are a helpful assistant" {
		t.Errorf("unexpected system message: %s", builder.systemMsg)
	}
}

func TestMultiModalBuilder_WithParameters(t *testing.T) {
	builder := NewMultiModalBuilder(
		context.Background(),
		testhelpers.NewMockLLM(),
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).
		WithTemperature(0.5).
		WithMaxTokens(500).
		WithTopP(0.9).
		WithModel("custom-model")

	if *builder.temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", *builder.temperature)
	}

	if *builder.maxTokens != 500 {
		t.Errorf("expected max tokens 500, got %d", *builder.maxTokens)
	}

	if *builder.topP != 0.9 {
		t.Errorf("expected top-p 0.9, got %f", *builder.topP)
	}

	if builder.model != "custom-model" {
		t.Errorf("expected model 'custom-model', got %s", builder.model)
	}
}

func TestMultiModalBuilder_WithCallbacks(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "Response",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	startCalled := false
	completeCalled := false

	builder := NewMultiModalBuilder(
		context.Background(),
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	).
		WithText("test").
		OnStart(func() {
			startCalled = true
		}).
		OnComplete(func(*MultiModalResult) {
			completeCalled = true
		})

	_, err := builder.Execute()
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !startCalled {
		t.Error("expected OnStart callback to be called")
	}

	if !completeCalled {
		t.Error("expected OnComplete callback to be called")
	}
}

func TestVisionAnalyzer_DescribeImage(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "This is a photo of a sunset over the ocean.",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	analyzer := NewVisionAnalyzer(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	imageData := []byte("fake image data")

	description, err := analyzer.DescribeImage(context.Background(), imageData, "image/jpeg")
	if err != nil {
		t.Fatalf("DescribeImage failed: %v", err)
	}

	if description != "This is a photo of a sunset over the ocean." {
		t.Errorf("unexpected description: %s", description)
	}
}

func TestVisionAnalyzer_DetectObjects(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "person, car, tree, building, bicycle",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	analyzer := NewVisionAnalyzer(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	imageData := []byte("fake image data")

	objects, err := analyzer.DetectObjects(context.Background(), imageData, "image/jpeg")
	if err != nil {
		t.Fatalf("DetectObjects failed: %v", err)
	}

	expectedObjects := []string{"person", "car", "tree", "building", "bicycle"}
	if len(objects) != len(expectedObjects) {
		t.Errorf("expected %d objects, got %d", len(expectedObjects), len(objects))
	}

	for i, obj := range objects {
		if obj != expectedObjects[i] {
			t.Errorf("expected object %s, got %s", expectedObjects[i], obj)
		}
	}
}

func TestVisionAnalyzer_ReadText(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "Hello World\nWelcome to AI",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	analyzer := NewVisionAnalyzer(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	imageData := []byte("fake image with text")

	text, err := analyzer.ReadText(context.Background(), imageData, "image/png")
	if err != nil {
		t.Fatalf("ReadText failed: %v", err)
	}

	expectedText := "Hello World\nWelcome to AI"
	if text != expectedText {
		t.Errorf("expected text '%s', got '%s'", expectedText, text)
	}
}

func TestVisionAnalyzer_CompareImages(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "The second image has a brighter sky and more people.",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	analyzer := NewVisionAnalyzer(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	img1 := []byte("fake image 1")
	img2 := []byte("fake image 2")

	comparison, err := analyzer.CompareImages(context.Background(), img1, img2, "image/jpeg")
	if err != nil {
		t.Fatalf("CompareImages failed: %v", err)
	}

	if !strings.Contains(comparison, "brighter sky") {
		t.Errorf("unexpected comparison result: %s", comparison)
	}
}

func TestAudioTranscriber_Transcribe(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "Hello, this is a test transcription.",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	transcriber := NewAudioTranscriber(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	audioData := []byte("fake audio data")

	transcript, err := transcriber.Transcribe(context.Background(), audioData, "en")
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	expectedTranscript := "Hello, this is a test transcription."
	if transcript != expectedTranscript {
		t.Errorf("expected transcript '%s', got '%s'", expectedTranscript, transcript)
	}
}

func TestAudioTranscriber_TranscribeWithTimestamps(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLLM.ChatFunc = func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{
			Model: req.Model,
			Choices: []llm.ChatChoice{
				{
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: "[00:00] Hello world\n[00:05] This is a test\n[00:10] End of audio",
					},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	transcriber := NewAudioTranscriber(
		mockLLM,
		testhelpers.NewMockLogger(),
		testhelpers.NewMockMetrics(),
	)
	audioData := []byte("fake audio data")

	segments, err := transcriber.TranscribeWithTimestamps(context.Background(), audioData, "en")
	if err != nil {
		t.Fatalf("TranscribeWithTimestamps failed: %v", err)
	}

	if len(segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(segments))
	}

	expectedTexts := []string{"Hello world", "This is a test", "End of audio"}
	for i, seg := range segments {
		if seg.Text != expectedTexts[i] {
			t.Errorf("segment %d: expected text '%s', got '%s'", i, expectedTexts[i], seg.Text)
		}
	}
}

func TestParseTimestampedTranscript(t *testing.T) {
	input := "[00:00] First segment\n[00:05] Second segment\n[00:10] Third segment"
	segments := parseTimestampedTranscript(input)

	if len(segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(segments))
	}

	expectedTexts := []string{"First segment", "Second segment", "Third segment"}
	for i, seg := range segments {
		if seg.Text != expectedTexts[i] {
			t.Errorf("segment %d: expected text '%s', got '%s'", i, expectedTexts[i], seg.Text)
		}
	}
}

func TestParseTimestampedTranscript_EmptyLines(t *testing.T) {
	input := "[00:00] First\n\n[00:05] Second\n\n\n[00:10] Third"
	segments := parseTimestampedTranscript(input)

	if len(segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(segments))
	}
}

func TestParseTimestampedTranscript_NoTimestamps(t *testing.T) {
	input := "This is plain text without timestamps"
	segments := parseTimestampedTranscript(input)

	if len(segments) != 0 {
		t.Errorf("expected 0 segments, got %d", len(segments))
	}
}
