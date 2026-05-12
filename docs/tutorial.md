# !!!WIP!!!

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

TODO: split this step in two

Form handlers process POST/PUT/PATCH/DELETE requests. Use `router.FormResponse` for validation and preserving form state.

```go
r.Use(
    router.FormHandler("createPeep",
        router.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            content := r.FormValue("content")
            author := r.FormValue("author")

            // Create FormResponse to handle validation
            form := router.NewFormResponse()
            form.State["content"] = []string{content}
            form.State["author"] = []string{author}

            // Validate
            if content == "" {
                form.AddError("content", "Content is required")
            }
            if author == "" {
                form.AddError("author", "Author is required")
            }
            if form.HasErrors() {
                return form, nil // Re-render form with errors
            }

            // Create the peep
            peep, err := db.CreatePeep(content, author)
            if err != nil {
                return nil, router.Unexpected(err)
            }

            // Redirect after successful creation
            w.Header().Set("HX-Location", "/peeps/"+peep.ID)
            form.Result = peep
            return form, nil
        }),
    ),
)
```

Add this form to `templates/peeps/index.tmpl` or create a separate `templates/peeps/create.tmpl` for use with the `D-TEMPLATE` header.
Use `d_form_handler` to specify which handler to invoke:

```html
<form
  hx-post="/peeps"
  hx-vals='{"d_form_handler": "createPeep"}'
  class="space-y-4"
>
  <div>
    <textarea
      name="content"
      class="w-full p-2 border rounded {{if .form.HasError "content"}}border-red-500{{end}}"
      placeholder="What's happening?"
    >{{.form.Value "content" ""}}</textarea>
    {{if .form.HasError "content"}}
      <p class="text-red-500 text-sm">{{index (.form.Errors "content") 0}}</p>
    {{end}}
  </div>
  <div>
    <input
      name="author"
      class="w-full p-2 border rounded {{if .form.HasError "author"}}border-red-500{{end}}"
      placeholder="Your name"
      value="{{.form.Value "author" ""}}"
    />
    {{if .form.HasError "author"}}
      <p class="text-red-500 text-sm">{{index (.form.Errors "author") 0}}</p>
    {{end}}
  </div>
  <button type="submit" class="bg-blue-500 text-white px-4 py-2 rounded">
    Post Peep
  </button>
</form>
```

When a handler returns a `*router.FormResponse`:

- `{{.form}}` contains the full FormResponse with validation state
- `{{.result}}` contains `FormResponse.Result` (the created entity, if any)

For non-FormResponse returns, `{{.result}}` contains the raw return value. To skip layout rendering, you can use a [Layout Resolver](#layout-resolvers).

### 6. Use Template Headers for Dialogs

TODO: this step doesn't feel right

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
