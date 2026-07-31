# Dave API Reference

Reference documentation for Dave, a file-based router for Go.

## Table of Contents

- [Router](#router)
- [Configuration](#configuration)
- [Error Handling](#error-handling)
- [Middleware](#middleware)
- [Form Handling](#form-handling)
- [Layouts](#layouts)
- [Template Functions](#template-functions)
- [Request Lifecycle](#request-lifecycle)
- [Template Priority](#template-priority)
- [Headers](#headers)
- [Logging](#logging)
- [Advanced API](#advanced-api)
- [Template Data Reference](#template-data-reference)
- [Security Considerations](#security-considerations)

---

## Router

### NewRouter

Creates a new router with the given filesystem.

```go
func NewRouter(fs fs.FS) *Router
```

**Example:**

```go
router := dave.NewRouter(os.DirFS("templates"))
```

### Use

Registers configuration functions with the router. `ConfFunc` is a function type that configures the router - all of Dave's configuration helpers (`Config`, `Middleware`, `FormHandler`, etc.) return `ConfFunc`.

```go
func (router *Router) Use(configFunc ...ConfFunc)
```

**Example:**

```go
router.Use(
    dave.Config(&dave.Conf{DevMode: true}),
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            r = dave.SetValue(r, "app", appConfig)
            next.ServeHTTP(w, r)
        })
    }),
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
router := dave.NewRouter(fs)
if err := router.ScanTemplates(); err != nil {
    log.Fatal(err)  // Catch template errors early
}
http.ListenAndServe(":8080", router)
```

---

## Configuration

### Config

Sets router configuration options.

```go
func Config(c *Conf) ConfFunc
```

### Conf struct

| Field               | Type     | Default     | Description                       |
| ------------------- | -------- | ----------- | --------------------------------- |
| `DevMode`           | `bool`   | `false`     | Rescan templates on every request |
| `DefaultLayout`     | `string` | `"default"` | Layout name when none specified   |
| `TemplateExtension` | `string` | `".tmpl"`   | File extension for templates      |
| `MaxFormSize`       | `int64`  | `10MB`      | Max size for multipart forms      |

**Example:**

```go
router.Use(
    dave.Config(&dave.Conf{
        DevMode:           true,
        DefaultLayout:     "main",
        TemplateExtension: ".html",
        MaxFormSize:       10 << 20,  // 10MB
    }),
)
```

---

## Error Handling

### Builtin Error Types

Dave provides three builtin error types for common cases:

| Function                  | Status | Fallback Template                |
| ------------------------- | ------ | -------------------------------- |
| `BadRequest(cause error)` | 400    | `fallback/bad_request.tmpl`      |
| `NotFound(cause error)`   | 404    | `fallback/not_found.tmpl`        |
| `Unexpected(cause error)` | 500    | `fallback/unexpected_error.tmpl` |

Dave uses these internally:

- `BadRequest` is returned when form parsing fails (malformed form data)
- `NotFound` is returned when a request path doesn't match any template
- `Unexpected` is returned for template parsing errors, unregistered form handlers, and other internal errors

They can also be used in registered handlers:

```go
dave.Get(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
    id := dave.PathVariable(r, "id")
    if id == "" {
        return nil, dave.BadRequest(fmt.Errorf("id is required"))
    }
    user, err := db.GetUser(id)
    if err != nil {
        return nil, dave.Unexpected(err)
    }
    if user == nil {
        return nil, dave.NotFound(fmt.Errorf("user %s not found", id))
    }
    dave.SetValue(r, "data", user)
    return nil, nil
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

In addition to built-in errors, Dave allows registering of custom error types.

```go
func ErrorType(target error, status int, fallbackName string) ConfFunc
```

When an error matches (or wraps) the target error, Dave will:

1. Set the HTTP status code
2. Render `fallback/<fallbackName>.tmpl` if it exists
3. Otherwise return a plain text response

**Wrapped errors are supported.** Dave uses `errors.Unwrap()` to check the entire error chain, so `fmt.Errorf("failed: %w", ErrUnauthorized)` will still match `ErrUnauthorized`.

**Setup:**

```go
var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")

router.Use(
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

**In form handlers:**

```go
dave.Get(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
    user := auth.GetUser(r)
    if user == nil {
        return nil, ErrUnauthorized
    }
    if !user.HasPermission("admin") {
        return nil, fmt.Errorf("user %s lacks permission: %w", user.ID, ErrForbidden)
    }
    dave.SetValue(r, "data", user)
    return nil, nil
})
```

**In middleware (via SetValue):**

Custom error types also work with middleware-set context values. If a value set via `SetValue` is an object with methods that return errors, Dave maps them to the appropriate error type.

```go
type AuthService struct{}

func (a *AuthService) CurrentUser() (*User, error) {
    return nil, ErrUnauthorized
}

router.Use(
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            r = dave.SetValue(r, "auth", &AuthService{})
            next.ServeHTTP(w, r)
        })
    }),
)
```

In templates:

```html
<!-- Triggers unauthorized fallback if no user is logged in -->
{{.auth.CurrentUser.Name}}
```

When the template calls `.CurrentUser` and it returns `ErrUnauthorized`, Dave catches the error and renders `fallback/unauthorized.tmpl` with a 401 status code.

---

## Middleware

### Middleware

Registers middleware that runs before template rendering. Middleware can set context values using `SetValue` which are available in templates via `{{.name}}`.

```go
func Middleware(mw func(http.Handler) http.Handler) ConfFunc
```

**Example:**

```go
router.Use(
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization")
            user := auth.GetUserFromToken(token)
            r = dave.SetValue(r, "currentUser", user)
            r = dave.SetValue(r, "config", appConfig)
            next.ServeHTTP(w, r)
        })
    }),
)
```

**Template access:**

```html
<p>Welcome, {{.currentUser.Name}}</p>
<p>Version: {{.config.Version}}</p>
```

### SetValue

Sets a context value that will be available in templates as `{{.name}}`.

**Reserved keys:** The following keys are reserved and will cause a panic if used: `path_variables`, `form`, `error`, `content`.

```go
func SetValue(r *http.Request, key string, value any) *http.Request
```

**Example:**

```go
r = dave.SetValue(r, "user", currentUser)
r = dave.SetValue(r, "theme", "dark")
```

### GetValue

Retrieves a context value inside a form handler or middleware.

```go
func GetValue(r *http.Request, key string) any
```

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
    userService := dave.GetValue(r, "userService").(*UserService)
    user, err := userService.Create(r.FormValue("name"))
    if err != nil {
        return nil, err
    }
    dave.SetValue(r, "data", user)
    return nil, nil
})
```

### PathVariables

Returns all path variables for the current request. Available after path parsing, including in middleware.

```go
func PathVariables(r *http.Request) map[string]string
```

**Example:**

```go
dave.Middleware(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        pathVars := dave.PathVariables(r)
        if id := pathVars["id"]; id != "" {
            user := db.GetUser(id)
            r = dave.SetValue(r, "user", user)
        }
        next.ServeHTTP(w, r)
    })
})
```

### Template

Returns the resolved template name for the current request. Available in middleware after path parsing.

```go
func Template(r *http.Request) string
```

### Layout

Returns the resolved layout name for the current request. Available in middleware after layout resolution.

```go
func Layout(r *http.Request) string
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
func(w http.ResponseWriter, r *http.Request) (*Form, error)
```

**Example:**

```go
router.Use(
    dave.FormHandler("user",
        dave.Get(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
            id := dave.PathVariable(r, "id")
            user, err := db.GetUser(id)
            if err != nil {
                return nil, err
            }
            dave.SetValue(r, "data", user)
            return nil, nil
        }),
        dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
            user := db.CreateUser(r.FormValue("name"))
            dave.SetValue(r, "data", user)
            return nil, nil
        }),
        dave.Delete(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
            id := dave.PathVariable(r, "id")
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

### Form

For validation and state preservation, return a `*Form`:

```go
func NewForm() *Form
```

**Form fields:**

- `State` — `map[string][]string` for preserving form values
- `ValidationErrors` — Field validation errors

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
    form := dave.NewForm()

    // Preserve submitted values
    form.State["email"] = []string{r.FormValue("email")}
    // or
    form.State = r.Form

    // Validate
    if r.FormValue("email") == "" {
        form.AddError("email", "Email is required")
    }

    if form.HasErrors() {
        return form, nil  // Re-render with errors
    }

    // Success - use SetValue for result data
    user := db.CreateUser(r.FormValue("email"))
    dave.SetValue(r, "data", user)
    w.Header().Set("HX-Location", "/users/"+user.ID) // HTMX way to redirect after creating an entity
    return nil, nil
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

### Template Override

Handlers can override which template is rendered using `SetTemplate`:

```go
dave.FormHandler("editUser",
    dave.Get(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
        dave.SetTemplate(r, "edit")  // Render "edit" template instead of "index"
        dave.SetValue(r, "data", user)
        return nil, nil
    }),
)
```

**Template Resolution:**

1. First, Dave looks for the template relative to the current path directory
2. If not found, treats the name as a full template path

For example, if the request is to `/users/123` (matching `users/{id}/index.tmpl`) and the handler calls `SetTemplate(r, "edit")`:

1. First tries: `users/{id}/edit.tmpl`
2. If not found: `edit.tmpl`

### Direct HTML Output

Handlers can write HTML directly to the response using `w.Write()`. This bypasses template rendering and is useful for HTMX responses that return small fragments:

```go
dave.FormHandler("toggleLike",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
        count := db.ToggleLike(r.FormValue("id"))
        w.Write([]byte(fmt.Sprintf(`<span class="likes">%d</span>`, count)))
        return nil, nil
    }),
)

dave.FormHandler("deleteItem",
    dave.Delete(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
        db.DeleteItem(r.FormValue("id"))
        w.Write([]byte(""))  // Empty response removes element with hx-swap="outerHTML"
        return nil, nil
    }),
)
```

When a handler writes to the response, template rendering is skipped entirely. The `Content-Type` header is automatically set to `text/html; charset=utf-8`.

---

## Layouts

### Layout Files

Create layouts in `templates/layouts/`. The default layout is `layouts/default.tmpl`.

```html
<!-- templates/layouts/default.tmpl -->
<!DOCTYPE html>
<html>
  <head>
    <title>{{.config.Title}}</title>
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
router.Use(
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

1. Layout resolver function (highest)
2. `DefaultLayout` config
3. `"default"` (if exists)

Empty string = no layout. If a layout name is resolved but the template doesn't exist, Dave silently falls back to no layout.

---

## Template Functions

### Func

Registers a template function. The factory receives the current `*http.Request` for request context access.

```go
func Func(name string, factory func(*http.Request) any) ConfFunc
```

**Example:**

```go
router.Use(
    dave.Func("upper", func(r *http.Request) any {
        return func(s string) string {
            return strings.ToUpper(s)
        }
    }),
    dave.Func("formatDate", func(r *http.Request) any {
        return func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        }
    }),
    dave.Func("isAdmin", func(r *http.Request) any {
        return func() bool {
            user := dave.GetValue(r, "currentUser")
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

### Built-in Functions

Dave provides three built-in template functions for component composition:

| Function | Signature | Description |
| -------- | --------- | ----------- |
| `slots` | `slots` → `map[string]template.HTML` | Creates an empty slot map |
| `slot` | `slot $s "name" "template" data` → `map[string]template.HTML` | Renders a template into a named slot |
| `render` | `render $s "name"` → `template.HTML` | Outputs a slot's content |

**Example:**

```html
<!-- Component definition -->
{{define "card"}}
<div class="card">
  <header>{{render . "header"}}</header>
  <main>{{render . "body"}}</main>
</div>
{{end}}

<!-- Usage -->
{{define "my-header"}}<h2>{{.Title}}</h2>{{end}}
{{define "my-body"}}<p>{{.Text}}</p>{{end}}

{{$s := slots}}
{{$s = slot $s "header" "my-header" .}}
{{$s = slot $s "body" "my-body" .}}
{{template "card" $s}}
```

See [Component Slots](recipes.md#component-slots) for detailed usage patterns.

---

## Request Lifecycle

1. **Parse path** — Extract URL path
2. **Match template** — Find best match, extract path variables
3. **Resolve template name** — `D-TEMPLATE` header or `"index"`
4. **Resolve layout** — Header → resolver → default
5. **Run middleware** — Execute registered middleware (path variables available via `PathVariables(r)`)
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

| Header       | Purpose                                                               |
| ------------ | --------------------------------------------------------------------- |
| `D-TEMPLATE` | Override template name (default: `index`). Must be alphanumeric only. |

---

## Logging

Dave does not include built-in request logging. To add request logging, wrap the router with a middleware. See [Request Logging](recipes.md#request-logging) for examples.

---

## Advanced API

### PathVariable

Get a single path variable. Returns an empty string if called outside the request lifecycle.

```go
func PathVariable(r *http.Request, name string) string
```

**Example:**

```go
id := dave.PathVariable(r, "id")
```

### SetTemplate

Override which template is rendered for the current request. The name is resolved relative to the current path directory first, then as a full path.

```go
func SetTemplate(r *http.Request, name string)
```

**Example:**

```go
dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
    if hasErrors {
        dave.SetTemplate(r, "edit")  // Re-render edit form
        return form, nil
    }
    dave.SetValue(r, "data", result)
    return nil, nil
})
```

### FormResponse

Get the form state from the current request. Returns `nil` if no form handler was executed or if the handler didn't return a `*Form`.

```go
func FormResponse(r *http.Request) *Form
```

**Example:**

```go
dave.Func("formValue", func(r *http.Request) any {
    return func(name string) string {
        if form := dave.FormResponse(r); form != nil {
            return form.Value(name, "")
        }
        return ""
    }
})
```

---

## Template Data Reference

| Variable                     | Type            | Description                           |
| ---------------------------- | --------------- | ------------------------------------- |
| `{{.<name>}}`                | `any`           | Context values set via `SetValue`     |
| `{{.data}}`                  | `any`           | Convention: handler-provided data via `SetValue(r, "data", x)` |
| `{{.path_variables.<name>}}` | `string`        | URL path variables                    |
| `{{.form}}`                  | `*Form`         | Form state (if `*Form` returned)      |
| `{{.error}}`                 | `string`        | Error message (fallback templates)    |
| `{{.content}}`               | `template.HTML` | Page content (layout templates)       |

**Reserved keys:** The following keys cannot be used with `SetValue` and will panic: `path_variables`, `form`, `error`, `content`.

---

## Security Considerations

### DevMode in Production

**Don't use `DevMode: true` in production.** DevMode:

- Rescans templates on every request (slow)
- Could load malicious templates if the filesystem is writable
- Reveals detailed error messages including file paths

### Path Variables

Path variables are user-controlled input extracted from URLs. Take care when:

- Using path variables in JavaScript contexts (e.g., `<script>var id = "{{.path_variables.id}}";</script>`)
- Passing them to `template.HTML()` or similar unsafe functions  
- Using them in href/src attributes without proper URL encoding

### Thread Safety

Call `router.Use()` only during initialization before starting the server. Calling it after the server starts processing requests may cause race conditions.

### Other Security Considerations

- Add middleware to set headers like `X-Content-Type-Options`, `X-Frame-Options`, and `Content-Security-Policy`.
- Implement CSRF tokens via middleware and expose the token to templates via `SetValue`.
- Use middleware to implement rate limiting.

