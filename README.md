# Dave

A file-based router for Go, built for HTMX applications.

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

Create `templates/index.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>Welcome</title>
  </head>
  <body>
    <h1>Hello, Dave!</h1>
  </body>
</html>
```

Visit `http://localhost:8080/` to see your page.

## Globals

Globals provide a way to share data and services across all templates. They're evaluated on every request and have access to the request and render context.

```go
r.Use(
    // Data providers with request access
    router.Global("currentUser", func(render *router.Render) any {
        token := render.Request().Header.Get("Authorization")
        return auth.GetUserFromToken(token)
    }),
    router.Global("config", func(render *router.Render) any {
        return appConfig
    }),

    // Access path variables
    router.Global("userService", func(render *router.Render) any {
        return &UserService{db: db}
    }),
)
```

Access in templates: `{{.globals.currentUser.Name}}`, `{{.globals.config.AppName}}`

Access in handlers (see next section):

```go
userService := router.GlobalValue(r, "userService").(*UserService)
```

**When to use:**

- Data needed on most pages (current user, navigation, permissions, configuration)
- Services that handlers or templates need to access

## Form Handling

Form handlers process form submissions and return data for templates. Register them with `router.FormHandler()` and specify which HTTP methods they handle:

```go
r.Use(
    router.FormHandler("createUser",
        router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            // Process POST request
            return user, nil
        }),
    ),
)
```

Trigger a handler by including `d_form_handler` in your form:

```html
<form method="POST">
  <input type="hidden" name="d_form_handler" value="createUser" />
  <!-- form fields -->
</form>
```

For simple handlers that just return data without validation, return any value directly. Use `FormResponse` when you need form state preservation and validation errors.

### FormResponse

Register form handler:

```go
router.FormHandler("updateUser",
        router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            form := router.NewFormResponse()

            // Preserve submitted values
            formResponse.State = r.Form
            // or set explicitly
            form.State["name"] = []string{r.FormValue("name")}
            form.State["email"] = []string{r.FormValue("email")}

            // Validate
            if r.FormValue("name") == "" {
                form.AddError("name", "Name is required")
            }
            if r.FormValue("email") == "" {
                form.AddError("email", "Email is required")
            }

            if form.HasErrors() {
                return form, nil // Re-render with errors
            }

            // Process valid submission
            user, err := db.UpdateUser(r.FormValue("name"), r.FormValue("email"))
            if err != nil {
                return nil, router.Unexpected(err)
            }

            form.Result = user
            return form, nil
        }),
    )
```

Return `router.FormResponse` to handle form validation and preserve form state across submissions:

When returning `*FormResponse`:

- `{{.form}}` is populated with the FormResponse
- `{{.result}}` shorthand for `FormResponse.Result`

When returning any other type:

- `{{.result}}` contains the raw return value
- `{{.form}}` is nil

### Template Usage

When a handler returns `*router.FormResponse`, these methods are available in templates:

| Method                          | Returns    | Description                         |
| ------------------------------- | ---------- | ----------------------------------- |
| `{{.form.HasErrors}}`           | `bool`     | True if any validation errors exist |
| `{{.form.HasError "field"}}`    | `bool`     | True if field has validation error  |
| `{{.form.Errors "field"}}`      | `[]string` | Validation error messages for field |
| `{{.form.Value "field" "def"}}` | `string`   | First value for field, or default   |
| `{{.form.Values "field"}}`      | `[]string` | All values for field (multi-select) |
| `{{.form.Result}}`              | `any`      | The result data (same as .result)   |

### Form Parsing

Dave automatically parses forms before calling handlers:

- `application/x-www-form-urlencoded`: standard form parsing
- `multipart/form-data`: parsed with configurable max size (default 32MB)

Access form values via `r.FormValue("field")` or `r.Form`.

## Layouts

Layouts wrap page content with shared structure (headers, footers, navigation). Create layout files in `layouts/`:

`templates/layouts/default.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>My App</title>
  </head>
  <body>
    <nav><!-- navigation --></nav>
    <main>{{.content}}</main>
    <footer><!-- footer --></footer>
  </body>
</html>
```

The `{{.content}}` placeholder is replaced with the rendered page template.
The default layout (`layouts/default.tmpl`) is applied automatically if it exists. Configure a different default with `DefaultLayout` in [Configuration](#configuration).

### Layout Resolvers

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

### Layout Priority

1. `D-LAYOUT` header (highest priority)
2. Layout resolver function
3. Default layout (`layouts/default.tmpl`)

If the resolved layout doesn't exist, the template renders without a layout.

## Components (aka Go templates)

Reference other templates:

`templates/components/button.tmpl`:

```html
<button class="btn">{{.}}</button>
```

`templates/posts/index.tmpl`:

```html
{{template "components/button" "Click Me"}}
```

## Template Functions

Template functions have access to the render context via `Func`. Pass a factory function that receives `*Render` and returns the template function:

```go
r.Use(
    router.Func("upper", func(render *router.Render) any {
        return func(s string) string {
            return strings.ToUpper(s)
        }
    }),
    router.Func("formatMoney", func(render *router.Render) any {
        return func(cents int) string {
            return fmt.Sprintf("$%.2f", float64(cents)/100)
        }
    }),
)
```

Use in templates:

```html
<p>{{upper .user.Name}}</p>
<p>Total: {{formatMoney .order.TotalCents}}</p>
```

**i18n Example:**

Since `Func` has access to the render context, i18n becomes simple - the function can read the language from globals directly:

```go
// main.go
type Translations map[string]map[string]string // lang -> key -> value

func loadTranslations(dir string) Translations {
    translations := make(Translations)
    files, _ := os.ReadDir(dir)
    for _, file := range files {
        lang := strings.TrimSuffix(file.Name(), ".json")
        data, _ := os.ReadFile(filepath.Join(dir, file.Name()))
        var t map[string]string
        json.Unmarshal(data, &t)
        translations[lang] = t
    }
    return translations
}

translations := loadTranslations("translations")

// Global that detects language from Accept-Language header
r.Use(
    router.Global("lang", func(render *router.Render) any {
        acceptLang := render.Request().Header.Get("Accept-Language")
        if strings.HasPrefix(acceptLang, "de") {
            return "de"
        }
        return "en"
    }),
)

// i18n function reads language from render context
r.Use(
    router.Func("i18n", func(render *router.Render) any {
        return func(key string) string {
            lang := render.Globals()["lang"].(string)
            if t, ok := translations[lang]; ok {
                if val, ok := t[key]; ok {
                    return val
                }
            }
            return key
        }
    }),
)
```

Template usage - no need to pass language explicitly:

```html
<h1>{{i18n "welcome_message"}}</h1>
```

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

## Error Handling

### Built-in Error Types

Dave provides two error types that map to specific HTTP status codes and fallback templates:

| Error Type               | HTTP Status | Fallback Template                |
| ------------------------ | ----------- | -------------------------------- |
| `router.NotFound(err)`   | 404         | `fallback/not_found.tmpl`        |
| `router.Unexpected(err)` | 500         | `fallback/unexpected_error.tmpl` |

```go
// 404 - resource not found
return nil, router.NotFound(fmt.Errorf("user %s not found", id))

// 500 - unexpected error (also logged automatically)
return nil, router.Unexpected(fmt.Errorf("database error: %w", err))
```

### Fallback Templates

Create custom error pages:

- `templates/fallback/not_found.tmpl` - for NotFound errors
- `templates/fallback/unexpected_error.tmpl` - for Unexpected errors

Error templates receive `{{.error}}` with the error message.

### Logging

Unexpected errors are logged automatically. Each request gets a unique `request_id` for tracing. Use `router.LoggerFromContext(r.Context())` to get a logger with the request ID:

```go
logger := router.LoggerFromContext(r.Context())
logger.Info("processing request", "user_id", userID)
```

### Response Writer Rules

Handlers should only set headers and status codes—let Dave render the template. Writing to the response body is logged as an error and the write is discarded.

**What handlers can do:**

- Set response headers: `w.Header().Set("HX-Location", "/users")`
- Set status codes: `w.WriteHeader(http.StatusCreated)`
- Return data for templates: `return user, nil`

**What handlers should NOT do:**

- Write to response body: `w.Write([]byte("..."))` (will be logged as error)

Handler return values are accessible in templates via `{{.result}}`:

```html
<!-- If handler returns a Post struct -->
<div class="p-4 border rounded">
  <p class="font-bold">{{.result.Title}}</p>
  <p>{{.result.Body}}</p>
</div>
```

## Request Lifecycle

Every request flows through these stages:

1. **Parse request path** - Extract the URL path from the request
2. **Match template** - Find the best matching template file, extracting path variables (e.g., `/posts/123` matches `posts/{id}/index.tmpl` with `id = "123"`)
3. **Resolve template name** - Check `D-TEMPLATE` header or default to `index`
4. **Resolve layout** - Priority: `D-LAYOUT` header → layout resolver → default layout
5. **Evaluate globals** - Call all registered global functions to populate `{{.globals}}`
6. **Parse form data** - Automatically parse `application/x-www-form-urlencoded` or `multipart/form-data`
7. **Execute form handler** - If `d_form_handler` is specified, run the matching handler for the HTTP method (use `router.GlobalValue()` and `router.PathVariable()` helpers)
8. **Build template data** - Assemble `path_variables`, `globals`, `result`, `form` (if FormResponse), and `error` into the template context
9. **Render template** - Execute the matched template with the assembled data
10. **Wrap in layout** - If a layout was resolved and exists, render it with `{{.content}}` containing the template output

## Template Priority

When multiple templates could match a path, explicit paths take precedence over path variables:

```
/users/new     → users/new/index.tmpl      (explicit match)
/users/123     → users/{id}/index.tmpl     (path variable)
/users/456/posts/latest → users/{id}/posts/latest/index.tmpl
/users/456/posts/789    → users/{id}/posts/{postId}/index.tmpl
```

## Headers Reference

| Header       | Purpose                                       |
| ------------ | --------------------------------------------- |
| `D-TEMPLATE` | Override the template name (default: `index`) |
| `D-LAYOUT`   | Override the layout                           |

## Developer Experience

### Dev Mode

When `DevMode: true`:

- Templates are rescanned on every request
- Changes to templates are reflected immediately without restart

### Auto-Reload with mise + Air

Use [mise](https://mise.jdx.dev) to manage tools and run the dev server with live reload:

```bash
mise install    # Install Go and Air
mise run dev    # Start dev server with live reload
```

The project includes `mise.toml` (tool definitions and tasks) and `.air.toml` (Air configuration). Air watches for Go file changes and automatically rebuilds/restarts the server.

With `DevMode: true`, template changes reload instantly without recompiling Go code.

## Advanced API

### Accessing Request Context

Use the shorthand helpers for common access patterns:

```go
router.FormHandler("example",
    router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        // Access path variables
        id := router.PathVariable(r, "id").(string)

        // Access globals
        userService := router.GlobalValue(r, "userService").(*UserService)

        return result, nil
    }),
)
```

For full access to the render context, use `router.GetRender()`:

```go
render := router.GetRender(r.Context())
template := render.Template()
layout := render.Layout()
pathVars := render.PathVariables()
globals := render.Globals()
```

### Render Type Methods

| Method            | Returns             | Description                |
| ----------------- | ------------------- | -------------------------- |
| `Template()`      | `string`            | The resolved template name |
| `PathVariables()` | `map[string]string` | Extracted path variables   |
| `Layout()`        | `string`            | The resolved layout name   |
| `Globals()`       | `map[string]any`    | Evaluated global values    |

### Path Variable Access

Access path variables in handlers using `router.PathVariable()`:

```go
id := router.PathVariable(r, "id").(string)
```

### Manual Template Scanning

Force a template rescan (useful for testing or custom reload logic):

```go
if err := r.ScanTemplates(); err != nil {
    log.Fatal(err)
}
```

### All HTTP Method Handlers

```go
router.FormHandler("resource",
    router.Get(handler),     // GET requests
    router.Post(handler),    // POST requests
    router.Put(handler),     // PUT requests
    router.Patch(handler),   // PATCH requests
    router.Delete(handler),  // DELETE requests
    router.MethodHandler("OPTIONS", handler), // Custom method
)
```

## Template Data Reference

Data available in templates:

| Variable                     | Type            | Description                                                        |
| ---------------------------- | --------------- | ------------------------------------------------------------------ |
| `{{.globals.<name>}}`        | `any`           | Values from Global providers                                       |
| `{{.path_variables.<name>}}` | `string`        | Extracted URL path variables                                       |
| `{{.result}}`                | `any`           | Return value from handler (or `FormResponse.Result`)               |
| `{{.form}}`                  | `*FormResponse` | Form state and validation errors (if handler returns FormResponse) |
| `{{.error}}`                 | `string`        | Error message (in fallback templates)                              |
| `{{.content}}`               | `string`        | Page content (in layout templates)                                 |

## Helpful Links

- [HTMX](https://htmx.org) - High power tools for HTML
- [HTMX Examples](https://htmx.org/examples/) - Implementation patterns
- [Hyperscript](https://hyperscript.org) - Client-side scripting
- [Alpine.js](https://alpinejs.dev) - Lightweight JS framework
