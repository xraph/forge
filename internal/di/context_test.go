package di

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test service for DI injection
type TestUserService struct {
	users []string
}

func (s *TestUserService) GetAll() []string {
	return s.users
}

func (s *TestUserService) GetByID(id string) string {
	for _, user := range s.users {
		if user == id {
			return user
		}
	}
	return ""
}

// Test request/response types
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestContext_RequestResponse(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	assert.Equal(t, req, ctx.Request())
	assert.Equal(t, rec, ctx.Response())
}

func TestContext_Params(t *testing.T) {
	req := httptest.NewRequest("GET", "/users/123", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("id", "123")

	assert.Equal(t, "123", ctx.Param("id"))

	params := ctx.Params()
	assert.Equal(t, "123", params["id"])
}

func TestContext_Query(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?name=john&age=30", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	assert.Equal(t, "john", ctx.Query("name"))
	assert.Equal(t, "30", ctx.Query("age"))
	assert.Equal(t, "", ctx.Query("missing"))
}

func TestContext_QueryDefault(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?name=john", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	assert.Equal(t, "john", ctx.QueryDefault("name", "default"))
	assert.Equal(t, "default", ctx.QueryDefault("missing", "default"))
}

func TestContext_BindJSON(t *testing.T) {
	type TestRequest struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var tr TestRequest
	err := ctx.BindJSON(&tr)
	require.NoError(t, err)

	assert.Equal(t, "John", tr.Name)
	assert.Equal(t, "john@example.com", tr.Email)
}

func TestContext_BindJSON_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var data map[string]string
	err := ctx.BindJSON(&data)
	assert.Error(t, err)
}

func TestContext_BindJSON_NilBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var data map[string]string
	err := ctx.BindJSON(&data)
	assert.Error(t, err)
}

func TestContext_BindXML(t *testing.T) {
	type TestRequest struct {
		XMLName xml.Name `xml:"request"`
		Name    string   `xml:"name"`
		Email   string   `xml:"email"`
	}

	body := `<request><name>John</name><email>john@example.com</email></request>`
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/xml")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var tr TestRequest
	err := ctx.BindXML(&tr)
	require.NoError(t, err)

	assert.Equal(t, "John", tr.Name)
	assert.Equal(t, "john@example.com", tr.Email)
}

func TestContext_Bind_AutoDetectJSON(t *testing.T) {
	type TestRequest struct {
		Name string `json:"name"`
	}

	body := `{"name":"John"}`
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var tr TestRequest
	err := ctx.Bind(&tr)
	require.NoError(t, err)
	assert.Equal(t, "John", tr.Name)
}

func TestContext_Bind_AutoDetectXML(t *testing.T) {
	type TestRequest struct {
		XMLName xml.Name `xml:"request"`
		Name    string   `xml:"name"`
	}

	body := `<request><name>John</name></request>`
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/xml")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var tr TestRequest
	err := ctx.Bind(&tr)
	require.NoError(t, err)
	assert.Equal(t, "John", tr.Name)
}

func TestContext_Bind_UnsupportedContentType(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte("data")))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	var data map[string]string
	err := ctx.Bind(&data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content type")
}

func TestContext_JSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	data := map[string]string{"message": "hello"}
	err := ctx.JSON(200, data)
	require.NoError(t, err)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, 200, rec.Code)

	var result map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "hello", result["message"])
}

func TestContext_XML(t *testing.T) {
	type TestResponse struct {
		XMLName xml.Name `xml:"response"`
		Message string   `xml:"message"`
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	data := TestResponse{Message: "hello"}
	err := ctx.XML(200, data)
	require.NoError(t, err)

	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
	assert.Equal(t, 200, rec.Code)
}

func TestContext_String(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	err := ctx.String(200, "hello world")
	require.NoError(t, err)

	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "hello world", rec.Body.String())
}

func TestContext_Bytes(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	data := []byte("binary data")
	err := ctx.Bytes(200, data)
	require.NoError(t, err)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "binary data", rec.Body.String())
}

func TestContext_NoContent(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	err := ctx.NoContent(204)
	require.NoError(t, err)

	assert.Equal(t, 204, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestContext_Redirect(t *testing.T) {
	req := httptest.NewRequest("GET", "/old", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	err := ctx.Redirect(302, "/new")
	require.NoError(t, err)

	assert.Equal(t, 302, rec.Code)
	assert.Equal(t, "/new", rec.Header().Get("Location"))
}

func TestContext_Redirect_InvalidCode(t *testing.T) {
	req := httptest.NewRequest("GET", "/old", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	err := ctx.Redirect(200, "/new")
	assert.Error(t, err)
}

func TestContext_Header(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom", "value")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	assert.Equal(t, "value", ctx.Header("X-Custom"))
	assert.Equal(t, "", ctx.Header("Missing"))
}

func TestContext_SetHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	ctx.SetHeader("X-Custom", "value")
	assert.Equal(t, "value", rec.Header().Get("X-Custom"))
}

func TestContext_SetGet(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	ctx.Set("key", "value")
	assert.Equal(t, "value", ctx.Get("key"))
	assert.Nil(t, ctx.Get("missing"))
}

func TestContext_MustGet(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	ctx.Set("key", "value")
	assert.Equal(t, "value", ctx.MustGet("key"))
}

func TestContext_MustGet_Panic(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	assert.Panics(t, func() {
		ctx.MustGet("missing")
	})
}

func TestContext_Context(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	reqCtx := ctx.Context()
	assert.NotNil(t, reqCtx)
}

func TestContext_WithContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	newCtx := context.WithValue(context.Background(), "key", "value")
	ctx.WithContext(newCtx)

	assert.Equal(t, "value", ctx.Context().Value("key"))
}

func TestContext_Container(t *testing.T) {
	container := NewContainer()
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container)

	assert.Equal(t, container, ctx.Container())
}

func TestContext_Scope(t *testing.T) {
	container := NewContainer()
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container)

	scope := ctx.Scope()
	assert.NotNil(t, scope)
}

func TestContext_Resolve(t *testing.T) {
	container := NewContainer()

	err := RegisterSingleton(container, "service", func(c Container) (*TestUserService, error) {
		return &TestUserService{users: []string{"user1"}}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container)

	svc, err := ctx.Resolve("service")
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestContext_Resolve_NoContainer(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	_, err := ctx.Resolve("service")
	assert.Error(t, err)
}

func TestContext_Must(t *testing.T) {
	container := NewContainer()

	err := RegisterSingleton(container, "service", func(c Container) (*TestUserService, error) {
		return &TestUserService{}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container)

	svc := ctx.Must("service")
	assert.NotNil(t, svc)
}

func TestContext_Must_Panic(t *testing.T) {
	container := NewContainer()
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container)

	assert.Panics(t, func() {
		ctx.Must("missing")
	})
}

func TestContext_Cleanup(t *testing.T) {
	container := NewContainer()
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, container).(*Ctx)

	// Cleanup should not panic
	ctx.cleanup()
}

// Multipart Form Tests

func TestContext_FormFile(t *testing.T) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add a file
	fileWriter, err := writer.CreateFormFile("avatar", "test.txt")
	require.NoError(t, err)
	_, err = fileWriter.Write([]byte("test file content"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Test FormFile
	file, header, err := ctx.FormFile("avatar")
	require.NoError(t, err)
	assert.NotNil(t, file)
	assert.NotNil(t, header)
	assert.Equal(t, "test.txt", header.Filename)

	// Read file content
	content := make([]byte, header.Size)
	_, err = file.Read(content)
	require.NoError(t, err)
	assert.Equal(t, "test file content", string(content))

	file.Close()
}

func TestContext_FormFile_NotFound(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	err := writer.Close()
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	_, _, err = ctx.FormFile("nonexistent")
	assert.Error(t, err)
}

func TestContext_FormFiles_Multiple(t *testing.T) {
	// Create multipart form with multiple files
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add first file
	fileWriter1, err := writer.CreateFormFile("documents", "doc1.txt")
	require.NoError(t, err)
	_, err = fileWriter1.Write([]byte("document 1"))
	require.NoError(t, err)

	// Add second file
	fileWriter2, err := writer.CreateFormFile("documents", "doc2.txt")
	require.NoError(t, err)
	_, err = fileWriter2.Write([]byte("document 2"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Test FormFiles
	files, err := ctx.FormFiles("documents")
	require.NoError(t, err)
	assert.Len(t, files, 2)
	assert.Equal(t, "doc1.txt", files[0].Filename)
	assert.Equal(t, "doc2.txt", files[1].Filename)
}

func TestContext_FormFiles_NotFound(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	err := writer.Close()
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	_, err = ctx.FormFiles("nonexistent")
	assert.Error(t, err)
}

func TestContext_FormValue(t *testing.T) {
	// Create multipart form with fields
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add form fields
	err := writer.WriteField("name", "John Doe")
	require.NoError(t, err)
	err = writer.WriteField("email", "john@example.com")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Test FormValue
	name := ctx.FormValue("name")
	assert.Equal(t, "John Doe", name)

	email := ctx.FormValue("email")
	assert.Equal(t, "john@example.com", email)
}

func TestContext_FormValues(t *testing.T) {
	// Create multipart form with multiple values for same field
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add multiple values for same field
	err := writer.WriteField("tags", "go")
	require.NoError(t, err)
	err = writer.WriteField("tags", "rust")
	require.NoError(t, err)
	err = writer.WriteField("tags", "python")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Parse form first
	err = ctx.ParseMultipartForm(32 << 20)
	require.NoError(t, err)

	// Test FormValues
	tags := ctx.FormValues("tags")
	assert.Len(t, tags, 3)
	assert.Contains(t, tags, "go")
	assert.Contains(t, tags, "rust")
	assert.Contains(t, tags, "python")
}

func TestContext_ParseMultipartForm(t *testing.T) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	err := writer.WriteField("field", "value")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Test ParseMultipartForm
	err = ctx.ParseMultipartForm(10 << 20) // 10MB
	assert.NoError(t, err)

	// Verify form was parsed
	value := ctx.FormValue("field")
	assert.Equal(t, "value", value)
}

func TestContext_ParseMultipartForm_InvalidForm(t *testing.T) {
	// Create invalid multipart form
	body := bytes.NewBufferString("not a multipart form")

	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----invalid")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	err := ctx.ParseMultipartForm(10 << 20)
	assert.Error(t, err)
}

func TestContext_Bind_MultipartForm(t *testing.T) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	err := writer.Close()
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Bind should return error for multipart (should use FormFile/FormValue)
	var data map[string]string
	err = ctx.Bind(&data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multipart/form-data should be handled using FormFile() and FormValue()")
}

func TestContext_Bind_URLEncoded(t *testing.T) {
	// Create URL-encoded form
	body := bytes.NewBufferString("name=John&email=john@example.com")

	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil)

	// Bind should parse the form
	err := ctx.Bind(nil)
	assert.NoError(t, err)

	// Verify form was parsed
	name := ctx.FormValue("name")
	assert.Equal(t, "John", name)
}

func TestContext_Cleanup_MultipartForm(t *testing.T) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add a file
	fileWriter, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = fileWriter.Write([]byte("test content"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	// Parse form
	err = ctx.ParseMultipartForm(10 << 20)
	require.NoError(t, err)

	// Cleanup should remove multipart form temp files
	ctx.cleanup()

	// After cleanup, multipart form should be cleaned up
	// We can't directly verify temp files were deleted, but ensure no panic
	assert.NotPanics(t, func() {
		ctx.cleanup()
	})
}
