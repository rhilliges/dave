TODO:
- test for adding globals to layouts
- test for //index error

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

## Tutorial: Building Peeps

Let's build a simple Twitter clone called "Peeps" to learn Dave's features.

### 1. Set Up the Layout and Homepage

First, create a layout that wraps all pages. Create `templates/layouts/default.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>Peeps</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
  </head>
  <body class="bg-gray-100 min-h-screen">
    <nav class="bg-white shadow-sm mb-4">
      <div class="max-w-2xl mx-auto p-4">
        <a href="/peeps" class="text-xl font-bold text-blue-500">Peeps</a>
      </div>
    </nav>
    {{.content}}
  </body>
</html>
```

And add a simple template at `templates/peeps/index.tmpl`:

```html
<div class="max-w-2xl mx-auto p-4">
  <h1 class="text-2xl font-bold mb-4">Timeline</h1>
  <p class="text-gray-500">No peeps yet. Check back later!</p>
</div>
```

Visit `/peeps` to see your page wrapped in the layout.

### 2. Add Global Data and the Timeline

Globals provide data and services to all templates. Register a `PeepService` that templates can use to fetch data:

```go
type PeepService struct {
    // TODO use an in-memory slice instead
    db *sql.DB
}

func (s *PeepService) GetRecent(limit int) []*Peep {
    // Return recent peeps from database
    return peeps
}

func (s *PeepService) GetByID(id string) (*Peep, error) {
    // Load peep from database
    return peep, nil
}

r.Use(
    router.Global("peepService", func() any {
        return &PeepService{db: db}
    }),
)
```

Now update `templates/peeps/index.tmpl` to display the timeline. Note: we're using `{{.CreatedAt}}` for now—we'll add a `date` formatter in the next step.

```html
<div class="max-w-2xl mx-auto p-4">
  <h1 class="text-2xl font-bold mb-4">Timeline</h1>
  {{range .globals.peepService.GetRecent 50}}
  <a
    href="/peeps/{{.ID}}"
    class="block border-b border-gray-200 py-4 hover:bg-gray-50"
  >
    <div class="flex items-center gap-2 mb-2">
      <span class="font-semibold">{{.Author}}</span>
      <span class="text-gray-500 text-sm">{{.CreatedAt}}</span>
    </div>
    <p class="text-gray-800">{{.Content}}</p>
  </a>
  {{else}}
  <p class="text-gray-500">No peeps yet. Check back later!</p>
  {{end}}
</div>
```

TODO: user needs to recompile. introduce DevMode here

### 3. Format Data with Template Functions

```go
r.Use(
        router.Func("date", func(t time.Time) string {
            return t.Format("Jan 2, 2006")
            }),
     )
```

Change the template to format the date: `{{.CreatedAt | date}}`

### 4. Use Path Variables

Create `templates/peeps/{id}/index.tmpl` for viewing a single peep:

```html
<div class="max-w-2xl mx-auto p-4">
  <a href="/peeps" class="text-blue-500 hover:underline mb-4 inline-block"
    >← Back to Timeline</a
  >
  {{with .globals.peepService.GetByID .path_variables.id}}
  <div class="mt-4 p-4 border rounded bg-white">
    <div class="flex items-center gap-2 mb-2">
      <span class="font-semibold">{{.Author}}</span>
      <span class="text-gray-500 text-sm">{{.CreatedAt}}</span>
    </div>
    <p class="text-gray-800">{{.Content}}</p>
  </div>
  {{else}}
  <p class="text-gray-500">Peep not found</p>
  {{end}}
</div>
```

`/peeps/123` renders this template with `id = "123"`. The template uses `peepService.GetByID` (registered in section 2) to load the peep data.

### 5. Handle Form Submissions

Form handlers process POST/PUT/PATCH/DELETE requests.

TODO: show how to do validation

```go
r.Use(
    router.FormHandler("createPeep",
        router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            content := r.FormValue("content")
            author := r.FormValue("author")
            peep, err := db.CreatePeep(content, author)
            if err != nil {
                return nil, router.Unexpected(err)
            }
            // Redirect after successful creation
            w.Header().Set("HX-Location", "/peeps/"+peep.ID)
            return peep, nil
        }),
    ),
)
```

Add this form to `templates/peeps/index.tmpl` or create a separate `templates/peeps/create.tmpl` for use with the `D-TEMPLATE` header:
Use `d_form_handler` to specify which handler to invoke:

```html
<form
  hx-post="/peeps"
  hx-vals='{"d_form_handler": "createPeep"}'
  class="space-y-4"
>
  <textarea
    name="content"
    required
    class="w-full p-2 border rounded"
    placeholder="What's happening?"
  ></textarea>
  <input
    name="author"
    required
    class="w-full p-2 border rounded"
    placeholder="Your name"
  />
  <button type="submit" class="bg-blue-500 text-white px-4 py-2 rounded">
    Post Peep
  </button>
</form>
```

The handler's return value is accessible in templates via `{{.handler_result}}`. To skip layout rendering, you can use a [Layout Resolver](#layout-resolvers).

TODO: this step doesn't feel right
### 6. Use Template Headers for Dialogs

Create `templates/peeps/create.tmpl` alongside `templates/peeps/index.tmpl`.

Request `/peeps` with header `D-TEMPLATE: create` to render the create template instead of index:

```html
<button
  hx-get="/peeps"
  hx-headers='{"D-TEMPLATE": "create"}'
  class="bg-blue-500 text-white px-4 py-2 rounded"
>
  New Peep
</button>
```

TODO: review stuff after this point

## Request Lifecycle

Every request flows through these stages:

1. **Parse request path** - Extract the URL path from the request
2. **Match template** - Find the best matching template file, extracting path variables (e.g., `/peeps/123` matches `peeps/{id}/index.tmpl` with `id = "123"`)
3. **Resolve template name** - Check `D-TEMPLATE` header or default to `index`
4. **Resolve layout** - Priority: `D-LAYOUT` header → layout resolver → default layout
5. **Evaluate globals** - Call all registered global functions to populate `{{.globals}}`
6. **Parse form data** - Automatically parse `application/x-www-form-urlencoded` or `multipart/form-data`
7. **Execute form handler** - If `d_form_handler` is specified, run the matching handler for the HTTP method (use `router.GlobalValue()` and `router.PathVariable()` helpers)
8. **Build template data** - Assemble `path_variables`, `globals`, `handler_result`, and `error` into the template context
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

## Globals

Globals provide shared data and services across all templates. They're evaluated on every request.

```go
r.Use(
    // Data providers
    router.Global("currentUser", func() any {
        return auth.GetCurrentUser()
    }),
    router.Global("config", func() any {
        return appConfig
    }),

    // Service registration
    router.Global("userService", func() any {
        return &UserService{db: db}
    }),
)
```

Access in templates: `{{.globals.currentUser.Name}}`, `{{.globals.config.AppName}}`

Access in handlers:

```go
userService := router.GlobalValue(r, "userService").(*UserService)
```

**When to use:**

- Data needed on most pages (current user, navigation, permissions)
- App-wide configuration
- Services that handlers or templates need to access

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
// translations.go
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

// main.go
translations := loadTranslations("translations")

// Register a global that detects language from Accept-Language header
r.Use(
    router.Global("lang", func() any {
        // Note: In a real app, you'd access the request context here
        return "en" // Default language
    }),
)

// Simple translation function (uses default language)
r.Use(
    router.Func("i18n", func(key string) string {
        lang := "en"
        if t, ok := translations[lang]; ok {
            if val, ok := t[key]; ok {
                return val
            }
        }
        return key
    }),
)
```

Template: `<h1>{{i18n "welcome_message"}}</h1>`

For language detection based on `Accept-Language`, use a global to provide the detected language and access it in templates:

## Headers Reference

| Header       | Purpose                                       |
| ------------ | --------------------------------------------- |
| `D-TEMPLATE` | Override the template name (default: `index`) |
| `D-LAYOUT`   | Override the layout                           |

## Layouts

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

## Error Handling

### Built-in Error Types

Dave provides two error types that map to specific HTTP status codes and fallback templates:

| Error Type               | HTTP Status | Fallback Template                |
| ------------------------ | ----------- | -------------------------------- |
| `router.NotFound(err)`   | 404         | `fallback/not_found.tmpl`        |
| `router.Unexpected(err)` | 500         | `fallback/unexpected_error.tmpl` |

```go
// 404 - resource not found
return nil, router.NotFound(fmt.Errorf("peep %s not found", id))

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

- Set response headers: `w.Header().Set("HX-Location", "/peeps")`
- Set status codes: `w.WriteHeader(http.StatusCreated)`
- Return data for templates: `return peep, nil`

**What handlers should NOT do:**

- Write to response body: `w.Write([]byte("..."))` (will be logged as error)

Handler return values are accessible in templates via `{{.handler_result}}`:

```html
<!-- If handler returns a Peep struct -->
<div class="p-4 border rounded">
  <p class="font-bold">{{.handler_result.Author}}</p>
  <p>{{.handler_result.Content}}</p>
</div>
```

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

| Option              | Default     | Description                         |
| ------------------- | ----------- | ----------------------------------- |
| `DevMode`           | `false`     | Rescan templates on every request   |
| `DefaultLayout`     | `"default"` | Layout used when none specified     |
| `TemplateExtension` | `".tmpl"`   | File extension for templates        |
| `MaxFormSize`       | `32MB`      | Max size for multipart form uploads |

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

## Components (aka Go templates)

Reference other templates:

`templates/components/button.tmpl`:

```html
<button class="btn">{{.}}</button>
```

`templates/peeps/index.tmpl`:

```html
{{template "components/button" "Click Me"}}
```

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

For full access to the render context, use `router.GetRequest()`:

```go
render := router.GetRequest(r.Context())
template := render.Template()
layout := render.Layout()
pathVars := render.PathVariables()
globals := render.Globals()
```

### Render Type Methods

| Method             | Returns             | Description                |
| ------------------ | ------------------- | -------------------------- |
| `Template()`       | `string`            | The resolved template name |
| `PathVariables()`  | `map[string]string` | Extracted path variables   |
| `Layout()`         | `string`            | The resolved layout name   |
| `Globals()`        | `map[string]any`    | Evaluated global values    |
| `ResolvedValues()` | `map[string]any`    | Handler return values      |

### Path Variable Access

Access path variables in handlers using `router.PathVariable()`:

```go
id := router.PathVariable(r, "id").(string)
```

### Global Value Access

Access global values in handlers using `router.GlobalValue()`:

```go
userService := router.GlobalValue(r, "userService").(*UserService)
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

| Variable                     | Type     | Description                           |
| ---------------------------- | -------- | ------------------------------------- |
| `{{.globals.<name>}}`        | `any`    | Values from Global providers          |
| `{{.path_variables.<name>}}` | `string` | Extracted URL path variables          |
| `{{.handler_result}}`        | `any`    | Return value from form handler        |
| `{{.error}}`                 | `string` | Error message (in fallback templates) |
| `{{.content}}`               | `string` | Page content (in layout templates)    |

## Helpful Links

- [HTMX](https://htmx.org) - High power tools for HTML
- [HTMX Examples](https://htmx.org/examples/) - Implementation patterns
- [Hyperscript](https://hyperscript.org) - Client-side scripting
- [Alpine.js](https://alpinejs.dev) - Lightweight JS framework
