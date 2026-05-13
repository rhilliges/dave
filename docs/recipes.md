# Recipes

This page contains practical recipes and patterns for common use cases with Dave.

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
    r := dave.NewRouter(templateFS)
    http.ListenAndServe(":8080", r)
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
  "new_user": "New User",
  "name": "Name",
  "email": "Email",
  "birthday": "Birthday",
  "create": "Create User",
  "back": "Back to Users",
  "joined": "Joined"
}
```

`translations/de.json`:

```json
{
  "welcome": "Willkommen",
  "users": "Benutzer",
  "new_user": "Neuer Benutzer",
  "name": "Name",
  "email": "E-Mail",
  "birthday": "Geburtstag",
  "create": "Benutzer erstellen",
  "back": "Zurück zu Benutzern",
  "joined": "Beigetreten"
}
```

### Loading Translations

Add translation loading to `main.go`:

```go
import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    // ... other imports
)

// Translations maps language code to key-value pairs
type Translations map[string]map[string]string

func loadTranslations(dir string) Translations {
    translations := make(Translations)
    files, _ := os.ReadDir(dir)
    for _, file := range files {
        if !strings.HasSuffix(file.Name(), ".json") {
            continue
        }
        lang := strings.TrimSuffix(file.Name(), ".json")
        data, _ := os.ReadFile(filepath.Join(dir, file.Name()))
        var t map[string]string
        json.Unmarshal(data, &t)
        translations[lang] = t
    }
    return translations
}
```

### Registering Globals and Functions

In `main()`, load translations and register the globals/functions:

```go
translations := loadTranslations("translations")

r.Use(
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

Update your templates to use the `t` function. For example, `templates/users/index.tmpl`:

```html
<h1 class="text-2xl font-bold mb-6">{{t "users"}}</h1>

<div class="grid gap-4">
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold mb-4">{{t "new_user"}}</h2>
    <form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium mb-1">{{t "name"}}</label>
          <input
            type="text"
            name="name"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">{{t "email"}}</label>
          <input
            type="email"
            name="email"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">{{t "birthday"}}</label>
          <input
            type="date"
            name="birthday"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <button
          type="submit"
          class="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700"
        >
          {{t "create"}}
        </button>
      </div>
    </form>
  </div>

  {{range .globals.users.All}}
  <a href="/users/{{.ID}}" class="block">
    {{template "components/user-card" .}}
  </a>
  {{else}}
  <p class="text-gray-500">No users yet.</p>
  {{end}}
</div>
```

And `templates/components/user-card.tmpl`:

```html
<div class="bg-white rounded-lg shadow p-6">
  <h2 class="text-xl font-semibold">{{.Name}}</h2>
  <p class="text-gray-600">{{.Email}}</p>
  <p class="text-gray-400 text-sm mt-2">
    {{t "joined"}}: {{.CreatedAt | formatDate}}
  </p>
</div>
```

### Testing

Test by setting your browser's language preference to German, or by adding a header:

```bash
curl -H "Accept-Language: de" http://localhost:8080/users
```

The page now displays in German. This pattern demonstrates how globals (for request-scoped state like detected language) and template functions (for transforming data) can work together to build powerful features.

## Function-Based Access

If you prefer accessing data via template functions instead of dot-notation, you can implement your own accessor functions. This can also be helpful when debugging:

```go
r.Use(
    dave.Func("var", func(render *dave.Render) any {
        return func(name string) string {
            logger := dave.LoggerFromContext(render.Request().Context())
            val := render.PathVariables()[name]
            logger.Debug("path variable accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("global", func(render *dave.Render) any {
        return func(name string) any {
            logger := dave.LoggerFromContext(render.Request().Context())
            val := render.Globals()[name]
            logger.Debug("global accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("form", func(render *dave.Render) any {
        return func() *dave.FormResponse {
            logger := dave.LoggerFromContext(render.Request().Context())
            form := render.FormResponse()
            logger.Debug("form accessed", "hasForm", form != nil)
            return form
        }
    }),
    dave.Func("field", func(render *dave.Render) any {
        return func(form *dave.FormResponse, name string) string {
            logger := dave.LoggerFromContext(render.Request().Context())
            if form == nil {
                logger.Debug("field accessed (no form)", "name", name)
                return ""
            }
            val := form.Value(name, "")
            logger.Debug("field accessed", "name", name, "value", val)
            return val
        }
    }),
    dave.Func("error", func(render *dave.Render) any {
        return func(form *dave.FormResponse, name string) []string {
            logger := dave.LoggerFromContext(render.Request().Context())
            if form == nil {
                logger.Debug("error accessed (no form)", "name", name)
                return nil
            }
            errs := form.Errors(name)
            logger.Debug("error accessed", "name", name, "errors", errs)
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
