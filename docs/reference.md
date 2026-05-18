# Dave API Reference

Reference documentation for Dave, a file-based router for Go.

## Table of Contents

- [Router](#router)
- [Configuration](#configuration)
- [Globals](#globals)
- [Form Handling](#form-handling)
- [Error Handling](#error-handling)
- [Layouts](#layouts)
- [Template Functions](#template-functions)
- [Request Lifecycle](#request-lifecycle)
- [Template Priority](#template-priority)
- [Headers](#headers)
- [Logging](#logging)
- [Advanced API](#advanced-api)
- [Template Data Reference](#template-data-reference)

---

## Router

### NewRouter

Creates a new router with the given filesystem.

```go
func NewRouter(fs fs.FS) *Router
```

**Example:**

```go
r := dave.NewRouter(os.DirFS("templates"))
```

### Use

Registers configuration functions with the router.

```go
func (router *Router) Use(configFunc ...ConfFunc)
```

**Example:**

```go
r.Use(
    dave.Config(&dave.Conf{DevMode: true}),
    dave.Global("app", func(r *dave.Render) any { return appConfig }),
    dave.FormHandler("submit", dave.Post(handler)),
)
```

### ScanTemplates

Manually scans templates at startup. Templates are normally scanned lazily on the first request.

```go
func (router *Router) ScanTemplates() error
```

**Example:**

```go
r := dave.NewRouter(fs)
if err := r.ScanTemplates(); err != nil {
    log.Fatal(err)  // Catch template errors early
}
http.ListenAndServe(":8080", r)
```

---

## Configuration

### Config

Sets router configuration options.

```go
func Config(c *Conf) ConfFunc
```

### Conf struct

| Field                | Type     | Default     | Description                                          |
| -------------------- | -------- | ----------- | ---------------------------------------------------- |
| `DevMode`            | `bool`   | `false`     | Rescan templates on every request                    |
| `DefaultLayout`      | `string` | `"default"` | Layout name when none specified                      |
| `TemplateExtension`  | `string` | `".tmpl"`   | File extension for templates                         |
| `MaxFormSize`        | `int64`  | `32MB`      | Max size for multipart forms                         |
| `AllowHandlerWrites` | `bool`   | `false`     | Allow handlers to write directly, skipping templates |

**Example:**

```go
r.Use(
    dave.Config(&dave.Conf{
        DevMode:           true,
        DefaultLayout:     "main",
        TemplateExtension: ".html",
        MaxFormSize:       10 << 20,  // 10MB
    }),
)
```

---

## Globals

### Global

Registers a global value provider. Globals are evaluated on every request.

```go
func Global(name string, globalFunc func(render *Render) any) ConfFunc
```

**Example:**

```go
r.Use(
    dave.Global("currentUser", func(render *dave.Render) any {
        token := render.Request().Header.Get("Authorization")
        return auth.GetUserFromToken(token)
    }),
    dave.Global("config", func(render *dave.Render) any {
        return appConfig
    }),
)
```

**Template access:**

```html
<p>Welcome, {{.globals.currentUser.Name}}</p>
<p>Version: {{.globals.config.Version}}</p>
```

### GlobalValue

Retrieves a global value in a form handler. Use this to access globals that were registered with `Global()`.

```go
func GlobalValue(r *http.Request, name string) any
```

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
    userService := dave.GlobalValue(r, "userService").(*UserService)
    return userService.Create(r.FormValue("name"))
})
```

---

## Form Handling

### FormHandler

Registers a named form handler with one or more HTTP method handlers.

```go
func FormHandler(name string, handlerFunc ...FormHandlerConfFunc) ConfFunc
```

### HTTP Method Helpers

| Function                         | HTTP Method |
| -------------------------------- | ----------- |
| `Get(handler)`                   | GET         |
| `Post(handler)`                  | POST        |
| `Put(handler)`                   | PUT         |
| `Patch(handler)`                 | PATCH       |
| `Delete(handler)`                | DELETE      |
| `MethodHandler(method, handler)` | Custom      |

**Handler signature:**

```go
func(w http.ResponseWriter, r *http.Request) (any, error)
```

**Example:**

```go
r.Use(
    dave.FormHandler("user",
        dave.Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
            id := dave.PathVariable(r, "id").(string)
            return db.GetUser(id)
        }),
        dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            user := db.CreateUser(r.FormValue("name"))
            return user, nil
        }),
        dave.Delete(func(w http.ResponseWriter, r *http.Request) (any, error) {
            id := dave.PathVariable(r, "id").(string)
            return nil, db.DeleteUser(id)
        }),
    ),
)
```

### Triggering Handlers

Include `d_form_handler` as a form field:

```html
<!-- Hidden input -->
<form method="POST">
  <input type="hidden" name="d_form_handler" value="createUser" />
  <!-- fields -->
</form>

<!-- HTMX with hx-vals -->
<form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
  <!-- fields -->
</form>
```

### FormResponse

For validation and state preservation, return a `*FormResponse`:

```go
func NewFormResponse() *FormResponse
```

**FormResponse fields:**

- `State` — `map[string][]string` for preserving form values
- `Errors` — Field validation errors
- `Result` — Success result data

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
    form := dave.NewFormResponse()

    // Preserve submitted values
    form.State["email"] = []string{r.FormValue("email")}

    // Validate
    if r.FormValue("email") == "" {
        form.AddError("email", "Email is required")
    }

    if form.HasErrors() {
        return form, nil  // Re-render with errors
    }

    // Success
    user := db.CreateUser(r.FormValue("email"))
    form.Result = user
    w.Header().Set("HX-Location", "/users/"+user.ID) // HTMX way to redirect after creating an entity
    return form, nil
})
```

**Template usage:**

| Method                              | Returns    | Description               |
| ----------------------------------- | ---------- | ------------------------- |
| `{{.form.HasErrors}}`               | `bool`     | Any validation errors?    |
| `{{.form.HasError "field"}}`        | `bool`     | Field has error?          |
| `{{.form.Errors "field"}}`          | `[]string` | Error messages for field  |
| `{{.form.Value "field" "default"}}` | `string`   | Field value or default    |
| `{{.form.Values "field"}}`          | `[]string` | All values (multi-select) |
| `{{.form.Result}}`                  | `any`      | Same as `{{.result}}`     |

**Template example:**

```html
<input
  name="email"
  value="{{.form.Value "email" ""}}"
  class="{{if .form.HasError "email"}}error{{end}}"
>
{{if .form.HasError "email"}}
  <span class="error">{{index (.form.Errors "email") 0}}</span>
{{end}}
```

### Response Writer Rules

By default, handlers can set headers and status codes but must NOT write to the response body. If a handler calls `w.Write()`, **Dave will panic**:

```
dave: form handlers must not write to the response body
```

#### AllowHandlerWrites

Set `AllowHandlerWrites: true` to let handlers write directly to the response, skipping template rendering. This is useful e.g. for HTMX responses that return small HTML fragments:

```go
r.Use(
    dave.Config(&dave.Conf{
        AllowHandlerWrites: true,
    }),
)

dave.FormHandler("deleteUser",
    dave.Delete(func(w http.ResponseWriter, r *http.Request) (any, error) {
        db.DeleteUser(r.FormValue("id"))
        w.Write([]byte("")) // Return empty to remove element
        return nil, nil
    }),
)

dave.FormHandler("toggleLike",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        count := db.ToggleLike(r.FormValue("id"))
        fmt.Fprintf(w, `<span class="likes">%d</span>`, count)
        return nil, nil
    }),
)
```

When `AllowHandlerWrites` is enabled:

- If handler writes to body → response is sent as-is, template rendering is skipped
- If handler doesn't write → template renders normally

---

## Error Handling

### Builtin Error Types

Dave provides two builtin error types for common cases:

| Function                  | Status | Fallback Template                |
| ------------------------- | ------ | -------------------------------- |
| `NotFound(cause error)`   | 404    | `fallback/not_found.tmpl`        |
| `Unexpected(cause error)` | 500    | `fallback/unexpected_error.tmpl` |

Dave uses these internally:

- `NotFound` is returned when a request path doesn't match any template
- `Unexpected` is returned for template parsing errors, unregistered form handlers, and other internal errors

They can also be used in registered handlers:

```go
dave.Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
    id := dave.PathVariable(r, "id").(string)
    user, err := db.GetUser(id)
    if err != nil {
        return nil, dave.Unexpected(err)
    }
    if user == nil {
        return nil, dave.NotFound(fmt.Errorf("user %s not found", id))
    }
    return user, nil
})
```

Create fallback templates in `templates/fallback/`:

```html
<!-- templates/fallback/not_found.tmpl -->
<h1>404 - Not Found</h1>
<p>{{.error}}</p>
<a href="/">Go Home</a>
```

### ErrorType

In addition to built in errors, Dave allows registering of custom error types.

```go
func ErrorType(target error, status int, fallbackName string) ConfFunc
```

When an error occurs (or an error wrapping the target), Dave will:

1. Set the HTTP status code
2. Render `fallback/<fallbackName>.tmpl` if it exists
3. Otherwise return a plain text response

**Setup:**

```go
var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")

r.Use(
    dave.ErrorType(ErrUnauthorized, http.StatusUnauthorized, "unauthorized"),
    dave.ErrorType(ErrForbidden, http.StatusForbidden, "forbidden"),
)
```

Create corresponding fallback templates:

```html
<!-- templates/fallback/unauthorized.tmpl -->
<h1>401 - Unauthorized</h1>
<p>Please <a href="/login">log in</a> to continue.</p>
```

Wrapped errors are supported—Dave unwraps errors to find matches.

**In form handlers:**

```go
dave.Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
    user := auth.GetUser(r)
    if user == nil {
        return nil, ErrUnauthorized
    }
    if !user.HasPermission("admin") {
        return nil, fmt.Errorf("user %s lacks permission: %w", user.ID, ErrForbidden)
    }
    return user, nil
})
```

**In globals:**

Custom error types also work with globals. If a global returns an object with methods that return errors, Dave maps them to the appropriate error type.

```go
type AuthService struct{}

func (a *AuthService) CurrentUser() (*User, error) {
    return nil, ErrUnauthorized
}

r.Use(
    dave.Global("auth", func(render *dave.Render) any {
        return &AuthService{}
    }),
)
```

In templates:

```html
<!-- Triggers unauthorized fallback if no user is logged in -->
{{.globals.auth.CurrentUser.Name}}
```

When the template calls `.CurrentUser` and it returns `ErrUnauthorized`, Dave catches the error and renders `fallback/unauthorized.tmpl` with a 401 status code.

---

## Layouts

### Layout Files

Create layouts in `templates/layouts/`. The default layout is `layouts/default.tmpl`.

```html
<!-- templates/layouts/default.tmpl -->
<!DOCTYPE html>
<html>
  <head>
    <title>{{.globals.config.Title}}</title>
  </head>
  <body>
    <nav><!-- navigation --></nav>
    <main>{{.content}}</main>
  </body>
</html>
```

### LayoutResolver

Dynamically choose layouts based on the request:

```go
func LayoutResolver(resolver LayoutResolverFunc) ConfFunc
```

**Example:**

```go
r.Use(
    dave.LayoutResolver(func(r *http.Request) string {
        // No layout for HTMX partial requests
        if r.Header.Get("HX-Request") == "true" {
            return ""
        }
        // Admin layout for admin routes
        if strings.HasPrefix(r.URL.Path, "/admin") {
            return "admin"
        }
        return "default"
    }),
)
```

### Layout Priority

1. `D-LAYOUT` header (highest)
2. Layout resolver function
3. `DefaultLayout` config
4. `"default"` (if exists)

Empty string = no layout.

---

## Template Functions

### Func

Registers a template function. The factory receives `*Render` for request context access.

```go
func Func(name string, factory func(*Render) any) ConfFunc
```

**Example:**

```go
r.Use(
    dave.Func("upper", func(render *dave.Render) any {
        return func(s string) string {
            return strings.ToUpper(s)
        }
    }),
    dave.Func("formatDate", func(render *dave.Render) any {
        return func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        }
    }),
    dave.Func("isAdmin", func(render *dave.Render) any {
        return func() bool {
            user := render.Globals()["currentUser"]
            return user != nil && user.(*User).IsAdmin
        }
    }),
)
```

**Template usage:**

```html
<h1>{{upper .title}}</h1>
<p>Created: {{.createdAt | formatDate}}</p>
{{if isAdmin}}<a href="/admin">Admin Panel</a>{{end}}
```

---

## Request Lifecycle

1. **Parse path** — Extract URL path
2. **Match template** — Find best match, extract path variables
3. **Resolve template name** — `D-TEMPLATE` header or `"index"`
4. **Resolve layout** — Header → resolver → default
5. **Evaluate globals** — Call all global functions
6. **Parse form** — Auto-parse form data
7. **Execute handler** — If `d_form_handler` specified
8. **Build data** — Assemble template context
9. **Render template** — Execute matched template
10. **Wrap in layout** — If layout resolved

---

## Template Priority

Explicit paths beat path variables:

```
/users/new     → users/new/index.tmpl      (explicit)
/users/123     → users/{id}/index.tmpl     (variable)
/users/123/posts/latest → users/{id}/posts/latest/index.tmpl
/users/123/posts/456    → users/{id}/posts/{postId}/index.tmpl
```

---

## Headers

| Header       | Purpose                                   |
| ------------ | ----------------------------------------- |
| `D-TEMPLATE` | Override template name (default: `index`) |
| `D-LAYOUT`   | Override layout                           |

---

## Logging

Dave does not include built-in request logging. To add request logging, wrap the router with a middleware. See [Request Logging](recipes.md#request-logging) for examples.

---

## Advanced API

### Render Type

The `Render` object contains all request context:

```go
type Render struct {
    // Methods:
    Request() *http.Request
    Template() string
    PathVariables() map[string]string
    Layout() string
    Globals() map[string]any
    HandlerResult() any
    FormResponse() *FormResponse  // nil if not FormResponse
}
```

### GetRender

Get full render context in handlers:

```go
func GetRender(ctx context.Context) Render
```

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
    render := dave.GetRender(r.Context())
    template := render.Template()
    layout := render.Layout()
    allGlobals := render.Globals()
    return nil, nil
})
```

### PathVariable

Get a single path variable:

```go
func PathVariable(r *http.Request, name string) any
```

**Example:**

```go
id := dave.PathVariable(r, "id").(string)
```

---

## Template Data Reference

| Variable                     | Type            | Description                           |
| ---------------------------- | --------------- | ------------------------------------- |
| `{{.globals.<name>}}`        | `any`           | Global values                         |
| `{{.path_variables.<name>}}` | `string`        | URL path variables                    |
| `{{.result}}`                | `any`           | Handler return value                  |
| `{{.form}}`                  | `*FormResponse` | Form state (if FormResponse returned) |
| `{{.error}}`                 | `string`        | Error message (fallback templates)    |
| `{{.content}}`               | `template.HTML` | Page content (layout templates)       |
