none

Removes a branch of the calendar save handler that could never run: it matched a form
`hx-post` on a retired path, and the day form submits with `hx-put` on `/api/v1/days/…`.
The grid refresh after a save is unchanged, and a test now pins that the editor is not
re-fetched.
