```
AI Disclaimer:
I used this project to find a good way for me to integrate AI into my workflow.
Couple things I learned:
- AI needs context (the right one); I ended up having to write the core logic w/o using any AI
- Its really good with tiny steps; once I had a good structure in place, I gave it small tasks to add features step by step. That worked really well.
- It likes to use fancy adjectives and verbs; I constantly had to tell it to stop using words like `comprehensive`.
```

# Dave

An framework agnostic file-based router for Go that works well with HTMX applications.
You just want a simple way to put a UI on top of your amazing Go CLI tool? Dave might is perfect for you.

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
- [**Globals**](#globals) — Share data and services across templates
- [**Form handlers**](#form-handlers) — Handle submissions with validation and error handling
- [**Layouts**](#layouts) — Wrap pages with shared headers, footers, navigation
- [**Error pages**](#error-handling) — Custom 404 and 500 templates with proper status codes
- [**Template functions**](#template-functions) — Add custom helpers/formatters like `formatDate`, `upper`, `i18n`
- [**Dev mode**](#configuration) — Hot reload templates without restarting the server
- [**HTMX-friendly**](#htmx-integration) — Layout resolver for partial requests, HX-Location redirects
- **Zero dependencies** — Just Go's standard library + one UUID package (because Go std library can't generate UUIDs; did not expect that)

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
    r := dave.NewRouter(os.DirFS("templates"))
    http.ListenAndServe(":8080", r)
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

### Globals

Share data across all templates:

```go
r.Use(
    dave.Global("currentUser", func(render *dave.Render) any {
        token := render.Request().Header.Get("Authorization")
        return auth.GetUser(token)
    }),
)
```

Access in templates: `{{.globals.currentUser.Name}}`

Register a service object with methods you can call from templates:

```go
r.Use(
    dave.Global("users", func(render *dave.Render) any {
        return userService  // has Get(id), All(), etc.
    }),
 )
```

```html
{{with .globals.users.Get .path_variables.id}}
<h1>{{.Name}}</h1>
<p>{{.Email}}</p>
{{end}}
```

Or access path variables to load data for the current page:

```go
r.Use(
    dave.Global("user", func(render *dave.Render) any {
        id := render.PathVariables()["id"]
        if id == "" {
            return nil
        }
        return db.GetUser(id)
    }),
)
```

Then in `templates/users/{id}/index.tmpl`:

```html
{{with .globals.user}}
<h1>{{.Name}}</h1>
<p>{{.Email}}</p>
{{else}}
<p>User not found</p>
{{end}}
```

### Form Handlers

Process form submissions:

```go
r.Use(
    dave.FormHandler("createPost",
        dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            title := r.FormValue("title")
            post := db.CreatePost(title)
            return post, nil
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

Handler results are available as `{{.result}}` in templates. See [Form Handling](docs/reference.md#form-handling) for validation, `FormResponse`, and more.

## Error Handling

Return typed errors for proper HTTP status codes:

```go
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

r.Use(
    dave.ErrorType(ErrUnauthorized, http.StatusUnauthorized, "unauthorized"),
)
```

See [Error Handling](docs/reference.md#error-handling) for custom error types, error handling in globals, and more.
```

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

## Template Functions

Add custom functions:

```go
r.Use(
    dave.Func("upper", func(render *dave.Render) any {
        return func(s string) string {
            return strings.ToUpper(s)
        }
    }),
)
```

Use in templates: `{{upper .user.Name}}`

## Configuration

```go
r.Use(
    dave.Config(&dave.Conf{
        DevMode:           true,     // Reload templates on every request
        DefaultLayout:     "main",   // Default: "default"
        TemplateExtension: ".html",  // Default: ".tmpl"
        MaxFormSize:       10 << 20, // Default: 32MB
    }),
)
```

## HTMX Integration

Dave works great with HTMX. Use a layout resolver to skip layouts for partial requests:

```go
r.Use(
    dave.LayoutResolver(func(r *http.Request) string {
        if r.Header.Get("HX-Request") == "true" {
            return "" // No layout for HTMX
        }
        return "default"
    }),
)
```

Redirect after form submission:

```go
dave.FormHandler("createUser",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        user := db.CreateUser(r.FormValue("name"))
        w.Header().Set("HX-Location", "/users/"+user.ID)
        return user, nil
    }),
)
```

## Components

Reuse templates with Go's built-in `{{template}}`:

```html
<!-- templates/components/button.tmpl -->
<button class="btn">{{.}}</button>

<!-- templates/posts/index.tmpl -->
{{template "components/button" "Click Me"}}
```

## Template Data Reference

| Variable                   | Description                           |
| -------------------------- | ------------------------------------- |
| `{{.globals.name}}`        | Global values                         |
| `{{.path_variables.name}}` | URL path variables                    |
| `{{.result}}`              | Form handler return value             |
| `{{.form}}`                | Form state (when using FormResponse)  |
| `{{.content}}`             | Page content (in layouts)             |
| `{{.error}}`               | Error message (in fallback templates) |

## Learn More

- **[Tutorial](docs/tutorial.md)** — Build a complete app step-by-step
- **[API Reference](docs/reference.md)** — Complete API documentation
- **[Recipes](docs/recipes.md)** — Patterns for i18n, embedding, and more
- **[HTMX](https://htmx.org)** — High power tools for HTML

## License

MIT
