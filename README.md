# Dave

A file-based router for Go that works well with HTMX applications.
You just want a simple way to put a UI on top of your amazing Go CLI tool? Dave is perfect for you.

**No route definitions needed**—just organize your Go template files in directories and Dave handles the rest.

```
templates/
├── index.tmpl           → /
├── about.tmpl           → /about
└── users/
    ├── index.tmpl       → /users
    └── {id}/
        └── index.tmpl   → /users/123  (id = "123")
```

## Features

- [**File-based routing**](#core-concepts) — URLs map to template files automatically
- [**Path variables**](#path-variables) — `/users/{id}` extracts `id` from the URL
- [**Middleware**](#middleware) — Share data and services across templates via context
- [**Form handlers**](#form-handlers) — Handle submissions with validation and error handling
- [**Layouts**](#layouts) — Wrap pages with shared headers, footers, navigation
- [**Error pages**](#error-handling) — Custom 404 and 500 templates with proper status codes
- [**Template functions**](#template-functions) — Add custom helpers/formatters like `formatDate`, `upper`, `i18n`
- [**Dev mode**](#configuration) — Hot reload templates without restarting the server
- [**HTMX-friendly**](#htmx-integration) — Layout resolver for partial requests, HX-Location redirects
- **Zero dependencies** — Just Go's standard library

## Installation

```bash
go get github.com/rhilliges/dave
```

## Quick Start

```go
package main

import (
    "net/http"
    "os"
    "github.com/rhilliges/dave"
)

func main() {
    router := dave.NewRouter(os.DirFS("templates"))
    http.ListenAndServe(":8080", router)
}
```

Create `templates/index.tmpl`:

```html
<!DOCTYPE html>
<html>
  <body>
    <h1>Hello, Dave!</h1>
  </body>
</html>
```

Visit `http://localhost:8080/` — that's it!

## Usage

### Path Variables

Use `{name}` in directory names to capture URL segments:

```
templates/users/{id}/index.tmpl  →  /users/123
```

Access in templates: `{{.path_variables.id}}`

### Middleware

Share data across all templates using middleware and context values:

```go
router.Use(
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization")
            user := auth.GetUser(token)
            r = dave.SetValue(r, "currentUser", user)
            next.ServeHTTP(w, r)
        })
    }),
)
```

Access in templates: `{{.currentUser.Name}}`

**Note:** The keys `path_variables`, `form`, `error`, and `content` are reserved and will panic if used with `SetValue`.

Register a service object with methods you can call from templates:

```go
router.Use(
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            r = dave.SetValue(r, "users", userService)
            next.ServeHTTP(w, r)
        })
    }),
)
```

```html
{{with .users.Get .path_variables.id}}
<h1>{{.Name}}</h1>
<p>{{.Email}}</p>
{{end}}
```

Or access path variables to load data for the current page:

```go
router.Use(
    dave.Middleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            pathVars := dave.PathVariables(r)
            if id := pathVars["id"]; id != "" {
                user := db.GetUser(id)
                r = dave.SetValue(r, "user", user)
            }
            next.ServeHTTP(w, r)
        })
    }),
)
```

Then in `templates/users/{id}/index.tmpl`:

```html
{{with .user}}
<h1>{{.Name}}</h1>
<p>{{.Email}}</p>
{{else}}
<p>User not found</p>
{{end}}
```

### Form Handlers

Process form submissions with validation:

```go
router.Use(
    dave.FormHandler("createPost",
        dave.Post(func(w http.ResponseWriter, r *http.Request) (*dave.Form, error) {
            title := r.FormValue("title")
            if title == "" {
                form := dave.NewForm()
                form.State = r.Form
                form.AddError("title", "Title is required")
                return form, nil
            }
            post := db.CreatePost(title)
            dave.SetValue(r, "data", post)
            return nil, nil
        }),
    ),
)
```

Trigger with a hidden input:

```html
<form method="POST">
  <input type="hidden" name="d_form_handler" value="createPost" />
  <input name="title" placeholder="Post title" />
  <button type="submit">Create</button>
</form>
```

Use `SetValue(r, "data", value)` to provide data to templates (accessible as `{{.data}}`). See [Form Handling](docs/reference.md#form-handling) for validation, `Form`, and more.

### Error Handling

Return typed errors for proper HTTP status codes:

```go
// 400 - renders fallback/bad_request.tmpl
return nil, dave.BadRequest(fmt.Errorf("invalid input"))

// 404 - renders fallback/not_found.tmpl
return nil, dave.NotFound(fmt.Errorf("user not found"))

// 500 - renders fallback/unexpected_error.tmpl
return nil, dave.Unexpected(err)
```

Create custom error pages in `templates/fallback/`:

```html
<!-- templates/fallback/not_found.tmpl -->
<h1>404 - Not Found</h1>
<p>{{.error}}</p>
```

Register custom error types for domain-specific errors:

```go
var ErrUnauthorized = errors.New("unauthorized")

router.Use(
    dave.ErrorType(ErrUnauthorized, http.StatusUnauthorized, "unauthorized"),
)
```

See [Error Handling](docs/reference.md#error-handling) for custom error types, error handling in middleware, and more.

### Layouts

Wrap pages with shared structure. Create `templates/layouts/default.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>My App</title>
  </head>
  <body>
    <nav><!-- navigation --></nav>
    <main>{{.content}}</main>
  </body>
</html>
```

Page templates automatically render inside `{{.content}}`.

### Template Functions

Add custom functions:

```go
router.Use(
    dave.Func("upper", func(r *http.Request) any {
        return func(s string) string {
            return strings.ToUpper(s)
        }
    }),
)
```

Use in templates: `{{upper .user.Name}}`

### Configuration

```go
router.Use(
    dave.Config(&dave.Conf{
        DevMode:           true,     // Reload templates on every request
        DefaultLayout:     "main",   // Default: "default"
        TemplateExtension: ".html",  // Default: ".tmpl"
        MaxFormSize:       32 << 20, // Default: 10MB
    }),
)
```

### Components

Reuse templates with Go's built-in `{{template}}`. Reference templates by their path (without extension):

```html
<!-- templates/components/button.tmpl -->
<button class="btn">{{.}}</button>

<!-- templates/posts/index.tmpl -->
{{template "components/button" "Click Me"}}
```

Any template can reference any other template by its full path:

```html
<!-- templates/users/profile/index.tmpl -->
{{template "components/avatar" .user}}
{{template "shared/sidebar" .}}
```

## Security

See [Security Considerations](docs/reference.md#security-considerations) for details.

## Learn More

- **[API Reference](docs/reference.md)** — Complete API documentation
- **[Recipes](docs/recipes.md)** — Patterns for i18n, embedding, and more
- **[HTMX](https://htmx.org)** — High power tools for HTML

## License

MIT
