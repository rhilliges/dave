# Dave

A file-based routing framework for Go, built for HTMX applications.

Dave maps URL paths to template files automatically. No route definitions needed—just organize your `.tmpl` files in directories.

## Quick Start

```go
package main

import (
    "net/http"
    "os"

    "github.com/yourusername/dave/internal/router"
)

func main() {
    fs := os.DirFS("templates")
    r := router.NewRouter(fs)
    http.ListenAndServe(":8080", r)
}
```

## Tutorial: Building an App

### 1. Create a Simple Index Page

Create `templates/users/index.tmpl`:

```html
<table>
  <tr>
    <th>Name</th>
    <th>Email</th>
  </tr>
  {{range .globals.users}}
  <tr>
    <td>{{.Name}}</td>
    <td>{{.Email}}</td>
  </tr>
  {{end}}
</table>
```

Visit `/users` to see your table.

### 2. Add a Global Data Provider

Globals provide data to all templates:

```go
r.Use(
    router.Global("users", func() any {
        return db.GetAllUsers()
    }),
)
```

Access in templates via `{{.globals.users}}`.

### 3. Use Path Variables

Create `templates/users/{id}/index.tmpl`:

```html
<h1>User: {{.path_variables.id}}</h1>
```

`/users/123` renders this template with `id = "123"`.

### 4. Load Data Using Path Variables

Use form handlers to fetch data based on path variables:

```go
r.Use(
    router.FormHandler("loadUser",
        router.Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
            id := router.VariableValue(r, "id").(string)
            user, err := db.GetUser(id)
            if err != nil {
                return nil, router.NotFound(err)
            }
            return user, nil
        }),
    ),
)
```

Template at `templates/users/{id}/index.tmpl`:

```html
<h1>{{.handler_result.Name}}</h1>
<p>Email: {{.handler_result.Email}}</p>
```

Request with `?d_form_handler=loadUser` to invoke the handler.

### 5. Format Data with Template Functions

```go
r.Use(
    router.Func("formatDate", func(t time.Time) string {
        return t.Format("Jan 2, 2006")
    }),
)
```

Use in templates: `{{.CreatedAt | formatDate}}`

### 6. Handle Form Submissions

Create handlers for POST/PUT/PATCH/DELETE:

```go
r.Use(
    router.FormHandler("createUser",
        router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            name := r.FormValue("name")
            email := r.FormValue("email")
            user, err := db.CreateUser(name, email)
            if err != nil {
                return nil, router.Unexpected(err)
            }
            // Redirect after successful creation
            w.Header().Set("HX-Location", "/users/"+user.ID)
            return user, nil
        }),
    ),
)
```

Form in template:

```html
<form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
    <input name="name" required>
    <input name="email" type="email" required>
    <button type="submit">Create</button>
</form>
```

### 7. Use Template Headers for Dialogs

Create `templates/users/create.tmpl` alongside `templates/users/index.tmpl`.

Request `/users` with header `D-TEMPLATE: create` to render the create template instead of index.

```html
<button hx-get="/users" hx-headers='{"D-TEMPLATE": "create"}'>
    New User
</button>
```

### 8. Use Layout Headers for Fullscreen Mode

Create `templates/layouts/fullscreen.tmpl`:

```html
<html>
<body class="fullscreen">{{.content}}</body>
</html>
```

Request with `D-LAYOUT: fullscreen` to use this layout.

## Request Lifecycle

1. Parse request path
2. Match path to template (with path variable extraction)
3. Resolve layout (header → resolver → default)
4. Parse form data
5. Execute form handler (if `d_form_handler` specified)
6. Evaluate globals
7. Render template
8. Wrap in layout (if applicable)

## Template Priority

When multiple templates could match a path, explicit paths take precedence over path variables:

```
/users/new     → users/new/index.tmpl      (explicit match)
/users/123     → users/{id}/index.tmpl     (path variable)
/users/456/posts/latest → users/{id}/posts/latest/index.tmpl
/users/456/posts/789    → users/{id}/posts/{postId}/index.tmpl
```

## Layout Priority

1. `D-LAYOUT` header (highest priority)
2. Layout resolver function
3. Default layout (`layouts/default.tmpl`)

If the resolved layout doesn't exist, the template renders without a layout.

## Globals

Use globals to provide shared data across templates:

```go
r.Use(
    router.Global("currentUser", func() any {
        return auth.GetCurrentUser()
    }),
    router.Global("config", func() any {
        return appConfig
    }),
)
```

Access: `{{.globals.currentUser.Name}}`, `{{.globals.config.AppName}}`

**When to use:**
- Data needed on most pages (current user, navigation, config)
- App-wide settings

## Template Functions

Add custom functions for use in templates:

```go
r.Use(
    router.Func("upper", strings.ToUpper),
    router.Func("formatMoney", func(cents int) string {
        return fmt.Sprintf("$%.2f", float64(cents)/100)
    }),
)
```

**i18n Example:**

```go
translations := loadTranslations("en.json")

r.Use(
    router.Func("t", func(key string) string {
        if val, ok := translations[key]; ok {
            return val
        }
        return key
    }),
)
```

Template: `<h1>{{t "welcome_message"}}</h1>`

## Headers Reference

| Header | Purpose |
|--------|---------|
| `D-TEMPLATE` | Override the template name (default: `index`) |
| `D-LAYOUT` | Override the layout |
| `HX-Request` | HTMX sets this; use with layout resolver to skip layouts |
| `HX-Location` | Set in handlers to trigger HTMX redirect |

## Layout Resolvers

Dynamically choose layouts based on the request:

```go
r.Use(
    router.LayoutResolver(func(r *http.Request) string {
        // Skip layout for HTMX partial requests
        if r.Header.Get("HX-Request") == "true" {
            return "" // empty string = no layout
        }
        return "default"
    }),
)
```

## Error Handling

### Built-in Error Types

```go
// 404 - resource not found
return nil, router.NotFound(fmt.Errorf("user %s not found", id))

// 500 - unexpected error
return nil, router.Unexpected(fmt.Errorf("database error: %w", err))
```

### Fallback Templates

Create custom error pages:

- `templates/fallback/not_found.tmpl` - for NotFound errors
- `templates/fallback/unexpected_error.tmpl` - for Unexpected errors

Error templates receive `{{.error}}` with the error message.

### Logging

Unexpected errors are logged automatically. Each request gets a unique `request_id` for tracing.

### Response Writer Rules

Handlers should only set headers and status codes. Writing to the response body is logged as an error—let Dave render the template.

## Form Parsing

Dave automatically parses forms before calling handlers:

- `application/x-www-form-urlencoded`: standard form parsing
- `multipart/form-data`: parsed with configurable max size (default 32MB)

Access form values via `r.FormValue("field")` or `r.Form`.

## Configuration

```go
r.Use(
    router.Config(&router.Conf{
        DevMode:           true,        // Rescan templates on each request
        DefaultLayout:     "main",      // Default: "default"
        TemplateExtension: ".html",     // Default: ".tmpl"
        MaxFormSize:       10 << 20,    // Default: 32MB
    }),
)
```

### Config Options

| Option | Default | Description |
|--------|---------|-------------|
| `DevMode` | `false` | Rescan templates on every request |
| `DefaultLayout` | `"default"` | Layout used when none specified |
| `TemplateExtension` | `".tmpl"` | File extension for templates |
| `MaxFormSize` | `32MB` | Max size for multipart form uploads |

## Developer Experience

### Dev Mode

When `DevMode: true`:
- Templates are rescanned on every request
- Changes to templates are reflected immediately without restart

### Auto-Reload with Air

Use [Air](https://github.com/cosmtrek/air) for live reloading. Configure `.air.toml` to only watch Go files if you're using dev mode for templates:

```toml
[build]
  cmd = "go build -o ./tmp/main ."
  include_ext = ["go"]
  exclude_dir = ["templates"]
```

With `DevMode: true`, template changes reload instantly without recompiling Go code.

## Nested Templates (Components)

Reference other templates:

`templates/components/button.tmpl`:
```html
<button class="btn">{{.}}</button>
```

`templates/users/index.tmpl`:
```html
{{template "components/button" "Click Me"}}
```

## Helpful Links

- [HTMX](https://htmx.org) - High power tools for HTML
- [HTMX Examples](https://htmx.org/examples/) - Implementation patterns
- [Hyperscript](https://hyperscript.org) - Client-side scripting
- [Alpine.js](https://alpinejs.dev) - Lightweight JS framework
