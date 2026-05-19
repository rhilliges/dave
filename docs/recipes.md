# Recipes

This page contains practical recipes and patterns for common use cases with Dave.

## HTMX Integration

Dave works great with HTMX.

Use a layout resolver to skip layouts for partial requests:

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

Set `HX-Location` headers to trigger client-side redirects after form submissions:

```go
dave.FormHandler("createUser",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        user := db.CreateUser(r.FormValue("name"))
        w.Header().Set("HX-Location", "/users/"+user.ID)
        return user, nil
    }),
)
```

Use `hx-vals` to pass the form handler name:

```html
<form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
  <input type="text" name="name" placeholder="Name" />
  <button type="submit">Create</button>
</form>
```

For simple HTMX responses that don't need a template, enable `AllowHandlerWrites` to return HTML fragments directly:

```go
router.Use(
    dave.Config(&dave.Conf{
        AllowHandlerWrites: true,
    }),
    dave.FormHandler("toggleLike",
        dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            count := db.ToggleLike(r.FormValue("id"))
            fmt.Fprintf(w, `<span class="likes">%d</span>`, count)
            return nil, nil
        }),
    ),
    dave.FormHandler("deleteItem",
        dave.Delete(func(w http.ResponseWriter, r *http.Request) (any, error) {
            db.DeleteItem(r.FormValue("id"))
            w.Write([]byte("")) // Return empty to remove element with hx-swap="outerHTML"
            return nil, nil
        }),
    ),
)

```

## Open Dialogs with D-TEMPLATE

Use the `D-TEMPLATE` header to render different templates for the same URL. This is useful for modals and dialogs:

```html
<!-- Button that opens a modal -->
<button
  hx-get="/users/123"
  hx-headers='{"D-TEMPLATE": "edit-modal"}'
  hx-target="#modal-container"
>
  Edit User
</button>
```

Create both templates:

```
templates/users/{id}/
├── index.tmpl       → Default view
└── edit-modal.tmpl  → Modal content
```

The `D-TEMPLATE` value must be alphanumeric (with hyphens and underscores allowed) for security.

## Dynamic Layout Selection

Use a `LayoutResolver` to dynamically select layouts based on request properties. This is useful for fullscreen views, print layouts, or client-controlled layout switching.

### Layout Override via Header

To allow clients to request a specific layout (similar to the removed `D-LAYOUT` header), implement a custom header check in your resolver:

```go
router.Use(
    dave.LayoutResolver(func(r *http.Request) string {
        // Allow client to request a specific layout
        if layout := r.Header.Get("X-Layout"); layout != "" {
            // Validate against allowed layouts for security
            switch layout {
            case "fullscreen", "print", "minimal":
                return layout
            }
        }
        // Default layout logic
        if r.Header.Get("HX-Request") == "true" {
            return "" // No layout for HTMX partials
        }
        return "default"
    }),
)
```

Use with HTMX:

```html
<!-- Fullscreen view button -->
<button
  hx-get="/dashboard"
  hx-headers='{"X-Layout": "fullscreen"}'
  hx-target="body"
  hx-push-url="true"
>
  Fullscreen Mode
</button>
```

## Embedding Templates

For single-binary deployment, use Go's `embed` package to bundle templates into the executable:

```go
package main

import (
    "embed"
    "io/fs"
    "net/http"
    "github.com/rhilliges/dave"
)

//go:embed templates/*
var templates embed.FS

func main() {
    templateFS, _ := fs.Sub(templates, "templates")
    router := dave.NewRouter(templateFS)
    http.ListenAndServe(":8080", router)
}
```

## Request Logging

Add your own logging middleware to match your application's needs:

```go
package main

import (
    ...

    "github.com/google/uuid"
    "github.com/rhilliges/dave"
)

func Logger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        requestID := uuid.New().String()

        logger := slog.Default().With(
            "request_id", requestID,
            "method", r.Method,
            "path", r.URL.Path,
        )

        logger.Info("request started")
        next.ServeHTTP(w, r)
        logger.Info("request completed", "duration_ms", time.Since(start).Milliseconds())
    })
}

func main() {
    router := dave.NewRouter(os.DirFS("templates"))
    http.ListenAndServe(":8080", Logger(router))
}
```

For response status logging, wrap the `http.ResponseWriter`:

```go
type responseWriter struct {
    http.ResponseWriter
    status int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
}

func Logger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

        next.ServeHTTP(rw, r)

        slog.Info("request",
            "method", r.Method,
            "path", r.URL.Path,
            "status", rw.status,
            "duration_ms", time.Since(start).Milliseconds(),
        )
    })
}
```

## Internationalization (i18n)

Add multi-language support using globals and template functions together.

### Translation Files

Create a `translations` directory with JSON files for each language:

`translations/en.json`:

```json
{
  "welcome": "Welcome",
  "users": "Users",
  ...
}
```

`translations/de.json`:

```json
{
  "welcome": "Willkommen",
  "users": "Benutzer",
  ...
}
```

### Loading Translations

Add translation loading to `main.go`:

```go
import (
    "encoding/json"
    "log"
    "os"
    "path/filepath"
    "strings"
    // ... other imports
)

// Translations maps language code to key-value pairs
type Translations map[string]map[string]string

func loadTranslations(dir string) Translations {
    translations := make(Translations)
    files, err := os.ReadDir(dir)
    if err != nil {
        log.Printf("warning: could not read translations directory: %v", err)
        return translations
    }
    for _, file := range files {
        if !strings.HasSuffix(file.Name(), ".json") {
            continue
        }
        lang := strings.TrimSuffix(file.Name(), ".json")
        data, err := os.ReadFile(filepath.Join(dir, file.Name()))
        if err != nil {
            log.Printf("warning: could not read %s: %v", file.Name(), err)
            continue
        }
        var t map[string]string
        if err := json.Unmarshal(data, &t); err != nil {
            log.Printf("warning: could not parse %s: %v", file.Name(), err)
            continue
        }
        translations[lang] = t
    }
    return translations
}
```

### Registering Globals and Functions

In `main()`, load translations and register the globals/functions:

```go
translations := loadTranslations("translations")

router.Use(
    // Detect language from Accept-Language header
    dave.Global("lang", func(render *dave.Render) any {
        acceptLang := render.Request().Header.Get("Accept-Language")
        if strings.HasPrefix(acceptLang, "de") {
            return "de"
        }
        return "en"
    }),

    // i18n function looks up translations using the detected language
    dave.Func("t", func(render *dave.Render) any {
        return func(key string) string {
            lang := render.Globals()["lang"].(string)
            if t, ok := translations[lang]; ok {
                if val, ok := t[key]; ok {
                    return val
                }
            }
            return key // Return key if translation not found
        }
    }),

    // ... rest of your config
)
```

### Using Translations in Templates

Use the `t` function in your templates:

```html
<h1>{{t "welcome"}}</h1>
<a href="/users">{{t "users"}}</a>
```

### Testing

Test by setting your browser's language preference to German, or by adding a header:

```bash
curl -H "Accept-Language: de" http://localhost:8080/users
```

The page now displays in German. This pattern demonstrates how globals (for request-scoped state like detected language) and template functions (for transforming data) can work together.

## Debugging Template Data Access

If you prefer accessing data via template functions instead of dot-notation, you can implement your own accessor functions. This can be helpful when debugging.

```go
router.Use(
    dave.Func("var", func(render *dave.Render) any {
        return func(name string) string {
            val := render.PathVariables()[name]
            slog.Debug("path variable accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("global", func(render *dave.Render) any {
        return func(name string) any {
            val := render.Globals()[name]
            slog.Debug("global accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("form", func(render *dave.Render) any {
        return func() *dave.FormResponse {
            form := render.FormResponse()
            slog.Debug("form accessed", "hasForm", form != nil)
            return form
        }
    }),
    dave.Func("field", func(render *dave.Render) any {
        return func(form *dave.FormResponse, name string) string {
            if form == nil {
                slog.Debug("field accessed (no form)", "name", name)
                return ""
            }
            val := form.Value(name, "")
            slog.Debug("field accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("error", func(render *dave.Render) any {
        return func(form *dave.FormResponse, name string) []string {
            if form == nil {
                slog.Debug("error accessed (no form)", "name", name)
                return nil
            }
            errs := form.Errors(name)
            slog.Debug("error accessed", "name", name, "errors", errs)
            return errs
        }
    }),
)
```

Then use in templates:

```html
<!-- Instead of {{.path_variables.id}} -->
{{var "id"}}

<!-- Instead of {{.globals.currentUser}} -->
{{global "currentUser"}}

<!-- Instead of {{.form.Value "email" ""}} -->
{{form | field "email"}}

<!-- Instead of {{.form.Errors "email"}} -->
{{form | error "email"}}
```

