package golang

const serviceConfigTemplate = `
// ServiceConfigResolver reads service settings without a framework dependency.
type ServiceConfigResolver interface { GetString(key string, defaultValue ...string) string }

// WithServiceName enables the named service's environment defaults.
func WithServiceName(name string) ClientOption { return func(c *Client) { c.serviceName = name } }

// WithServiceConfig resolves services.<name>.url, timeout and retry.attempts at construction.
func WithServiceConfig(name string, resolver ServiceConfigResolver) ClientOption {
 return func(c *Client) { c.serviceName = name; c.serviceConfig = resolver }
}

// WithRetryMethods opts specific methods into retrying. Use it for writes only
// when the endpoint's idempotency contract makes replay safe.
func WithRetryMethods(methods ...string) ClientOption {
 return func(c *Client) { c.retryMethods=map[string]bool{}; for _,method:=range methods { c.retryMethods[strings.ToUpper(method)]=true } }
}

// NewClientChecked returns configuration errors before your first request.
func NewClientChecked(opts ...ClientOption) (*Client,error) {
 c:=NewClient(opts...)
 if c.configurationError!=nil { return nil,c.configurationError }
 return c,nil
}

// ConfigurationError exposes errors retained by the compatible NewClient constructor.
func (c *Client) ConfigurationError() error { return c.configurationError }

func (c *Client) resolveServiceConfig() error {
 if c.httpClient==nil { return fmt.Errorf("HTTP client is required") }
 clone:=*c.httpClient
 c.httpClient=&clone
 lookup:=func(field string) string {
 if c.serviceName=="" { return "" }
 if c.serviceConfig!=nil { if value:=c.serviceConfig.GetString("services."+c.serviceName+"."+field); value!="" { return value } }
 envField:=field
 if field=="retry.attempts" { envField="retries" }
 key:=strings.ToUpper(strings.NewReplacer("-","_",".","_").Replace(c.serviceName))+"_"+strings.ToUpper(envField)
 return os.Getenv(key)
 }
 if !c.baseURLExplicit { if endpoint:=lookup("url");endpoint!="" { c.baseURL=endpoint } }
 parsed,err:=url.Parse(c.baseURL)
 if err!=nil || (parsed.Scheme!="http" && parsed.Scheme!="https") || parsed.Host=="" || parsed.User!=nil || parsed.Fragment!="" || parsed.RawQuery!="" {
 return fmt.Errorf("service endpoint must be an absolute HTTP or HTTPS URL without credentials, query or fragment")
 }
 c.baseURL=strings.TrimRight(c.baseURL,"/")
 if c.timeoutExplicit {
 c.httpClient.Timeout=c.requestTimeout
 } else if value:=lookup("timeout");value!="" {
 duration,err:=time.ParseDuration(value)
 if err!=nil || duration<=0 { return fmt.Errorf("service timeout must be a positive duration") }
 c.httpClient.Timeout=duration
 }
 if value:=lookup("retry.attempts");value!="" {
 retries,err:=strconv.Atoi(value)
 if err!=nil || retries<0 || retries>5 { return fmt.Errorf("service retries must be between zero and five") }
 c.retries=retries
 }
 return nil
}

func (c *Client) retryAllowed(method string) bool {
 if c.retryMethods!=nil { return c.retryMethods[method] }
 return method==http.MethodGet || method==http.MethodHead || method==http.MethodOptions
}
`

const retryRequestTemplate = `
 var resp *http.Response
 for attempt:=0;;attempt++ {
 resp,err=c.httpClient.Do(req)
 if attempt>=c.retries || !c.retryAllowed(method) || err==nil && resp.StatusCode<500 && resp.StatusCode!=http.StatusTooManyRequests { break }
 if resp!=nil { _,_=io.CopyN(io.Discard,resp.Body,4096);_ = resp.Body.Close() }
 if req.Body!=nil {
 if req.GetBody==nil { return fmt.Errorf("request body cannot be replayed") }
 req.Body,err=req.GetBody()
 if err!=nil { return fmt.Errorf("reset retry request body: %w",err) }
 }
 timer:=time.NewTimer(time.Duration(1<<attempt)*100*time.Millisecond)
 select {
 case <-ctx.Done(): timer.Stop();if req.Body!=nil {_ = req.Body.Close()};return ctx.Err()
 case <-timer.C:
 }
 }
`
