# Tutorial: Building a User Management Service

In this tutorial, we'll build a simple user management service to learn Dave's core features.

## Prerequisites

- Go 1.21+
- Basic familiarity with Go and HTML templates

## 1. Hello World with Layout

Let's start with a simple page wrapped in a layout.

Create a new directory and initialize a Go module:

```bash
mkdir users-app && cd users-app
go mod init users-app
go get github.com/rhilliges/dave
```

Create `main.go`:

```go
package main

import (
    "log"
    "net/http"
    "os"

    "github.com/rhilliges/dave"
)

func main() {
    fs := os.DirFS("templates")
    r := dave.NewRouter(fs)

    // Enable DevMode for development - templates reload without server restart
    r.Use(dave.Config(&dave.Conf{DevMode: true}))

    log.Println("Server starting on http://localhost:8080")
    http.ListenAndServe(":8080", r)
}
```

> **Tip:** With `DevMode: true`, you can edit `.tmpl` files and refresh your browser to see changes immediately—no server restart needed. Disable this in production for better performance.

Create the layout at `templates/layouts/default.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>Users App</title>
    <script src="https://cdn.tailwindcss.com"></script>
  </head>
  <body class="bg-gray-100 min-h-screen">
    <nav class="bg-white shadow-sm mb-6">
      <div class="max-w-4xl mx-auto p-4">
        <a href="/" class="text-xl font-bold text-blue-600">Users App</a>
      </div>
    </nav>
    <main class="max-w-4xl mx-auto px-4">{{.content}}</main>
  </body>
</html>
```

Create `templates/index.tmpl`:

```html
<h1 class="text-2xl font-bold mb-4">Welcome</h1>
<p class="text-gray-600">A simple user management app built with Dave.</p>
```

Run the application:

```bash
go run main.go
```

Visit http://localhost:8080. Dave automatically maps `/` to `index.tmpl` and wraps it with the default layout.

Your project structure should now look like this:

```
users-app/
├── main.go
└── templates/
    ├── index.tmpl
    └── layouts/
        └── default.tmpl
```

## 2. Display Users with Globals

Globals provide shared data to all templates. Create a user store and display users as cards.

Update `main.go`:

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/rhilliges/dave"
)

type User struct {
    ID        string
    Name      string
    Email     string
    Birthday  time.Time
    CreatedAt time.Time
}

type UserStore struct {
    mu     sync.RWMutex
    users  map[string]*User
    nextID int
}

func NewUserStore() *UserStore {
    store := &UserStore{
        users:  make(map[string]*User),
        nextID: 1,
    }
    // Sample data
    store.Create("Alice Smith", "alice@example.com", time.Date(1990, 5, 15, 0, 0, 0, 0, time.UTC))
    store.Create("Bob Jones", "bob@example.com", time.Date(1985, 11, 22, 0, 0, 0, 0, time.UTC))
    return store
}

func (s *UserStore) All() []*User {
    s.mu.RLock()
    defer s.mu.RUnlock()
    users := make([]*User, 0, len(s.users))
    for _, u := range s.users {
        users = append(users, u)
    }
    return users
}

func (s *UserStore) Create(name, email string, birthday time.Time) *User {
    s.mu.Lock()
    defer s.mu.Unlock()
    id := fmt.Sprintf("%d", s.nextID)
    s.nextID++
    user := &User{
        ID:        id,
        Name:      name,
        Email:     email,
        Birthday:  birthday,
        CreatedAt: time.Now(),
    }
    s.users[id] = user
    return user
}

func main() {
    fs := os.DirFS("templates")
    r := dave.NewRouter(fs)

    store := NewUserStore()

    r.Use(
        dave.Global("users", func(render *dave.Render) any {
            return store
        }),
    )

    log.Println("Server starting on http://localhost:8080")
    http.ListenAndServe(":8080", r)
}
```

Create `templates/users/index.tmpl`:

```html
<h1 class="text-2xl font-bold mb-6">Users</h1>

<div class="grid gap-4">
  {{range .globals.users.All}}
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold">{{.Name}}</h2>
    <p class="text-gray-600">{{.Email}}</p>
    <p class="text-gray-400 text-sm mt-2">Joined: {{.CreatedAt}}</p>
  </div>
  {{else}}
  <p class="text-gray-500">No users yet.</p>
  {{end}}
</div>
```

Visit http://localhost:8080/users to see the user cards.

## 3. Format Dates with Template Functions

The `CreatedAt` date isn't formatted nicely. We can fix that with template functions.

Update `main.go` to register a template function:

```go
r.Use(
    dave.Global("users", func(render *dave.Render) any {
        return store
    }),
    dave.Func("formatDate", func(render *dave.Render) any {
        return func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        }
    }),
)
```

Update `templates/users/index.tmpl` to use the function:

```html
<h1 class="text-2xl font-bold mb-6">Users</h1>

<div class="grid gap-4">
  {{range .globals.users.All}}
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold">{{.Name}}</h2>
    <p class="text-gray-600">{{.Email}}</p>
    <p class="text-gray-400 text-sm mt-2">
      Joined: {{.CreatedAt | formatDate}}
    </p>
  </div>
  {{else}}
  <p class="text-gray-500">No users yet.</p>
  {{end}}
</div>
```

Refresh the page. The dates now display as "Jan 2, 2006" format.

## 4. Create Users with a Form

Add a form to create new users. The form will be displayed in a card, matching the list style.

Add a form handler to `main.go`:

```go
r.Use(
    dave.Global("users", func(render *dave.Render) any {
        return store
    }),
    dave.Func("formatDate", func(render *dave.Render) any {
        return func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        }
    }),
    dave.FormHandler("createUser",
        dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            name := r.FormValue("name")
            email := r.FormValue("email")
            birthday, _ := time.Parse("2006-01-02", r.FormValue("birthday"))

            user := store.Create(name, email, birthday)
            return user, nil
        }),
    ),
)
```

Update `templates/users/index.tmpl` to include the form. Note the hidden `d_form_handler` input—this tells Dave which handler to invoke when the form is submitted:

```html
<h1 class="text-2xl font-bold mb-6">Users</h1>

<div class="grid gap-4">
  <!-- Create User Form -->
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold mb-4">New User</h2>
    <form method="POST" action="/users">
      <input type="hidden" name="d_form_handler" value="createUser" />
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium mb-1">Name</label>
          <input
            type="text"
            name="name"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Email</label>
          <input
            type="email"
            name="email"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Birthday</label>
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
          Create User
        </button>
      </div>
    </form>
  </div>

  <!-- User List -->
  {{range .globals.users.All}}
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold">{{.Name}}</h2>
    <p class="text-gray-600">{{.Email}}</p>
    <p class="text-gray-400 text-sm mt-2">
      Joined: {{.CreatedAt | formatDate}}
    </p>
  </div>
  {{else}}
  <p class="text-gray-500">No users yet.</p>
  {{end}}
</div>
```

Try creating a user. The form submits, but the page just reloads showing the same list—we'll improve this with HTMX soon.

## 5. Extract a Reusable Card Component

The card styling is repeated across user cards. Let's extract it into a reusable component.

Create `templates/components/user-card.tmpl`:

```html
<div class="bg-white rounded-lg shadow p-6">
  <h2 class="text-xl font-semibold">{{.Name}}</h2>
  <p class="text-gray-600">{{.Email}}</p>
  <p class="text-gray-400 text-sm mt-2">Joined: {{.CreatedAt | formatDate}}</p>
</div>
```

Update `templates/users/index.tmpl`:

```html
<h1 class="text-2xl font-bold mb-6">Users</h1>

<div class="grid gap-4">
  <!-- Create User Form -->
  <div class="bg-white rounded-lg shadow p-6">
    <h2 class="text-xl font-semibold mb-4">New User</h2>
    <form method="POST" action="/users">
      <input type="hidden" name="d_form_handler" value="createUser" />
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium mb-1">Name</label>
          <input
            type="text"
            name="name"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Email</label>
          <input
            type="email"
            name="email"
            class="w-full border rounded px-3 py-2"
          />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Birthday</label>
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
          Create User
        </button>
      </div>
    </form>
  </div>

  <!-- User List -->
  {{range .globals.users.All}} {{template "components/user-card" .}} {{else}}
  <p class="text-gray-500">No users yet.</p>
  {{end}}
</div>
```

Now each user card uses the shared component. If you want to change the card styling, you only need to update it in one place.

## 6. Skip Layout for HTMX Requests

Before we add HTMX, let's set up a layout resolver. When HTMX sends a request, we typically only want the page content without the layout wrapper—this prevents the entire page from being replaced.

Update `main.go` to add a layout resolver before your other middleware:

```go
r.Use(
    dave.LayoutResolver(func(r *http.Request) string {
        if r.Header.Get("HX-Request") == "true" {
            return "" // No layout for HTMX requests
        }
        return "default"
    }),
    dave.Global("users", func(render *dave.Render) any {
        return store
    }),
    dave.Func("formatDate", func(render *dave.Render) any {
        return func(t time.Time) string {
            return t.Format("Jan 2, 2006")
        }
    }),
    dave.FormHandler("createUser",
        dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
            name := r.FormValue("name")
            email := r.FormValue("email")
            birthday, _ := time.Parse("2006-01-02", r.FormValue("birthday"))

            user := store.Create(name, email, birthday)
            return user, nil
        }),
    ),
)
```

Now HTMX requests receive only the page content, while regular browser requests still get the full layout.

## 7. Redirect with HTMX After Creation

Now let's add HTMX for a smoother user experience. First, add HTMX to the layout.

Update `templates/layouts/default.tmpl`:

```html
<!DOCTYPE html>
<html>
  <head>
    <title>Users App</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@2.0.4"></script>
  </head>
  <body class="bg-gray-100 min-h-screen">
    <nav class="bg-white shadow-sm mb-6">
      <div class="max-w-4xl mx-auto p-4">
        <a href="/" class="text-xl font-bold text-blue-600">Users App</a>
      </div>
    </nav>
    <main class="max-w-4xl mx-auto px-4">{{.content}}</main>
  </body>
</html>
```

Update the form handler in `main.go` to redirect after creation:

```go
dave.FormHandler("createUser",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        name := r.FormValue("name")
        email := r.FormValue("email")
        birthday, _ := time.Parse("2006-01-02", r.FormValue("birthday"))

        user := store.Create(name, email, birthday)

        // Tell HTMX to redirect to the user's page
        w.Header().Set("HX-Location", "/users/"+user.ID)
        return user, nil
    }),
),
```

Update the form to use HTMX. In `templates/users/index.tmpl`, change the form:

```html
<form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
  <div class="space-y-4">
    <div>
      <label class="block text-sm font-medium mb-1">Name</label>
      <input type="text" name="name" class="w-full border rounded px-3 py-2" />
    </div>
    <div>
      <label class="block text-sm font-medium mb-1">Email</label>
      <input
        type="email"
        name="email"
        class="w-full border rounded px-3 py-2"
      />
    </div>
    <div>
      <label class="block text-sm font-medium mb-1">Birthday</label>
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
      Create User
    </button>
  </div>
</form>
```

We also need a page to view individual users. Add a `Get` method to `UserStore`:

```go
func (s *UserStore) Get(id string) *User {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.users[id]
}
```

Create `templates/users/{id}/index.tmpl`:

```html
<a href="/users" class="text-blue-600 hover:underline mb-4 inline-block"
  >← Back to Users</a
>

{{with .globals.users.Get .path_variables.id}} {{template "components/user-card"
.}} {{else}}
<p class="text-gray-500">User not found.</p>
{{end}}
```

Now when you create a user, HTMX redirects to their profile page. Thanks to the layout resolver we set up earlier, the HTMX request only receives the page content.

## 8. Validate User Data with Field Errors

Now let's add validation. The birthday must be in the past.

Update the form handler in `main.go`:

```go
dave.FormHandler("createUser",
    dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
        form := dave.NewFormResponse()

        name := r.FormValue("name")
        email := r.FormValue("email")
        birthdayStr := r.FormValue("birthday")

        // Preserve form values
        form.State["name"] = []string{name}
        form.State["email"] = []string{email}
        form.State["birthday"] = []string{birthdayStr}

        if name == "" {
            form.AddError("name", "Name is required")
        }
        if email == "" {
            form.AddError("email", "Email is required")
        }

        var birthday time.Time
        if birthdayStr == "" {
            form.AddError("birthday", "Birthday is required")
        } else {
            var err error
            birthday, err = time.Parse("2006-01-02", birthdayStr)
            if err != nil {
                form.AddError("birthday", "Invalid date format")
            } else if birthday.After(time.Now()) {
                form.AddError("birthday", "Birthday must be in the past")
            }
        }

        if form.HasErrors() {
            return form, nil
        }

        user := store.Create(name, email, birthday)
        w.Header().Set("HX-Location", "/users/"+user.ID)
        form.Result = user
        return form, nil
    }),
),
```

Update the form in `templates/users/index.tmpl` to show errors and preserve values:

```html
<h1 class="text-2xl font-bold mb-6">Users</h1>

<div class="grid gap-4">
    <!-- Create User Form -->
    <div class="bg-white rounded-lg shadow p-6">
        <h2 class="text-xl font-semibold mb-4">New User</h2>
        <form hx-post="/users" hx-vals='{"d_form_handler": "createUser"}'>
            <div class="space-y-4">
                <div>
                    <label class="block text-sm font-medium mb-1">Name</label>
                    <input
                        type="text"
                        name="name"
                        value="{{.form.Value "name" ""}}"
                        class="w-full border rounded px-3 py-2 {{if .form.HasError "name"}}border-red-500{{end}}"
                    >
                    {{if .form.HasError "name"}}
                    <p class="text-red-500 text-sm mt-1">{{index (.form.Errors "name") 0}}</p>
                    {{end}}
                </div>
                <div>
                    <label class="block text-sm font-medium mb-1">Email</label>
                    <input
                        type="email"
                        name="email"
                        value="{{.form.Value "email" ""}}"
                        class="w-full border rounded px-3 py-2 {{if .form.HasError "email"}}border-red-500{{end}}"
                    >
                    {{if .form.HasError "email"}}
                    <p class="text-red-500 text-sm mt-1">{{index (.form.Errors "email") 0}}</p>
                    {{end}}
                </div>
                <div>
                    <label class="block text-sm font-medium mb-1">Birthday</label>
                    <input
                        type="date"
                        name="birthday"
                        value="{{.form.Value "birthday" ""}}"
                        class="w-full border rounded px-3 py-2 {{if .form.HasError "birthday"}}border-red-500{{end}}"
                    >
                    {{if .form.HasError "birthday"}}
                    <p class="text-red-500 text-sm mt-1">{{index (.form.Errors "birthday") 0}}</p>
                    {{end}}
                </div>
                <button type="submit" class="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700">
                    Create User
                </button>
            </div>
        </form>
    </div>

    <!-- User List -->
    {{range .globals.users.All}}
    {{template "components/user-card" .}}
    {{else}}
    <p class="text-gray-500">No users yet.</p>
    {{end}}
</div>
```

Try submitting the form with invalid data. The errors display inline, and thanks to the layout resolver we set up earlier, only the page content is swapped—no full page reload.

## 9. Handle Non-Existing Users

Currently, visiting `/users/999` shows "User not found" but returns a 200 status. Let's add a proper 404 page using a fallback template.

Create `templates/fallback/not_found.tmpl`:

```html
<div class="text-center py-12">
  <h1 class="text-4xl font-bold text-gray-300 mb-4">404</h1>
  <p class="text-gray-600 mb-6">{{.error}}</p>
  <a href="/users" class="text-blue-600 hover:underline">← Back to Users</a>
</div>
```

Now update the user detail template to return a 404 when the user doesn't exist. The trick is to use a global that can trigger the error. Update `main.go` to add a `getUser` global:

```go
r.Use(
    dave.LayoutResolver(func(r *http.Request) string {
        if r.Header.Get("HX-Request") == "true" {
            return ""
        }
        return "default"
    }),
    dave.Global("users", func(render *dave.Render) any {
        return store
    }),
    dave.Global("getUser", func(render *dave.Render) any {
        return func(id string) (*User, error) {
            user := store.Get(id)
            if user == nil {
                return nil, dave.NotFound(fmt.Errorf("user %s not found", id))
            }
            return user, nil
        }
    }),
    // ... rest of your config
)
```

Update `templates/users/{id}/index.tmpl` to use the new global:

```html
<a href="/users" class="text-blue-600 hover:underline mb-4 inline-block"
  >← Back to Users</a
>

{{with .globals.getUser .path_variables.id}} {{template "components/user-card"
.}} {{end}}
```

Now visiting `/users/999` displays the custom 404 page with the error message and returns a proper 404 status code.

Finally, let's make the user cards clickable. Update the user list in `templates/users/index.tmpl`:

```html
{{range .globals.users.All}}
<a href="/users/{{.ID}}" class="block">
  {{template "components/user-card" .}}
</a>
{{else}}
<p class="text-gray-500">No users yet.</p>
{{end}}
```

## Final Project Structure

Your completed project should look like this:

```
users-app/
├── main.go
└── templates/
    ├── index.tmpl
    ├── layouts/
    │   └── default.tmpl
    ├── components/
    │   └── user-card.tmpl
    ├── fallback/
    │   └── not_found.tmpl
    └── users/
        ├── index.tmpl
        └── {id}/
            └── index.tmpl
```

## Complete Code

Here's the final `main.go`:

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/rhilliges/dave"
)

type User struct {
    ID        string
    Name      string
    Email     string
    Birthday  time.Time
    CreatedAt time.Time
}

type UserStore struct {
    mu     sync.RWMutex
    users  map[string]*User
    nextID int
}

func NewUserStore() *UserStore {
    store := &UserStore{
        users:  make(map[string]*User),
        nextID: 1,
    }
    store.Create("Alice Smith", "alice@example.com", time.Date(1990, 5, 15, 0, 0, 0, 0, time.UTC))
    store.Create("Bob Jones", "bob@example.com", time.Date(1985, 11, 22, 0, 0, 0, 0, time.UTC))
    return store
}

func (s *UserStore) All() []*User {
    s.mu.RLock()
    defer s.mu.RUnlock()
    users := make([]*User, 0, len(s.users))
    for _, u := range s.users {
        users = append(users, u)
    }
    return users
}

func (s *UserStore) Get(id string) *User {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.users[id]
}

func (s *UserStore) Create(name, email string, birthday time.Time) *User {
    s.mu.Lock()
    defer s.mu.Unlock()
    id := fmt.Sprintf("%d", s.nextID)
    s.nextID++
    user := &User{
        ID:        id,
        Name:      name,
        Email:     email,
        Birthday:  birthday,
        CreatedAt: time.Now(),
    }
    s.users[id] = user
    return user
}

func main() {
    fs := os.DirFS("templates")
    r := dave.NewRouter(fs)

    // Enable DevMode for development
    r.Use(dave.Config(&dave.Conf{DevMode: true}))

    store := NewUserStore()

    r.Use(
        dave.LayoutResolver(func(r *http.Request) string {
            if r.Header.Get("HX-Request") == "true" {
                return ""
            }
            return "default"
        }),
        dave.Global("users", func(render *dave.Render) any {
            return store
        }),
        dave.Global("getUser", func(render *dave.Render) any {
            return func(id string) (*User, error) {
                user := store.Get(id)
                if user == nil {
                    return nil, dave.NotFound(fmt.Errorf("user %s not found", id))
                }
                return user, nil
            }
        }),
        dave.Func("formatDate", func(render *dave.Render) any {
            return func(t time.Time) string {
                return t.Format("Jan 2, 2006")
            }
        }),
        dave.FormHandler("createUser",
            dave.Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
                form := dave.NewFormResponse()

                name := r.FormValue("name")
                email := r.FormValue("email")
                birthdayStr := r.FormValue("birthday")

                form.State["name"] = []string{name}
                form.State["email"] = []string{email}
                form.State["birthday"] = []string{birthdayStr}

                if name == "" {
                    form.AddError("name", "Name is required")
                }
                if email == "" {
                    form.AddError("email", "Email is required")
                }

                var birthday time.Time
                if birthdayStr == "" {
                    form.AddError("birthday", "Birthday is required")
                } else {
                    var err error
                    birthday, err = time.Parse("2006-01-02", birthdayStr)
                    if err != nil {
                        form.AddError("birthday", "Invalid date format")
                    } else if birthday.After(time.Now()) {
                        form.AddError("birthday", "Birthday must be in the past")
                    }
                }

                if form.HasErrors() {
                    return form, nil
                }

                user := store.Create(name, email, birthday)
                w.Header().Set("HX-Location", "/users/"+user.ID)
                form.Result = user
                return form, nil
            }),
        ),
    )

    log.Println("Server starting on http://localhost:8080")
    http.ListenAndServe(":8080", r)
}
```

## Next Steps

You've learned Dave's core features:

- **File-based routing** - URLs map to template files
- **Layouts** - Wrap pages with common structure
- **Globals** - Share data and services across templates
- **Template functions** - Transform data in templates
- **Path variables** - Extract dynamic segments from URLs
- **Form handlers** - Process form submissions
- **FormResponse** - Handle validation and preserve form state
- **Layout resolvers** - Dynamically choose layouts (great for HTMX)
- **Error handling** - Return proper errors with fallback templates

Explore more:

- [API Reference](reference.md) - Configuration options, headers, and advanced API
- [Recipes](recipes.md) - Practical patterns like i18n
