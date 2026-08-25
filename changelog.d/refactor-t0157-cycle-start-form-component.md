### Internal

- **The manual cycle-start control and its confirmation policy are one
  component.** The form and the hidden `[data-cycle-start-policy]` node beside it
  were copied four times — three branches of the calendar day panel and the
  dashboard journal — and the eleven attributes on that node are the whole
  contract with the browser script that stands between a mis-tap and replacing a
  recorded cycle start. All four copies were complete, so nothing rendered wrong;
  what the duplication cost was that adding a twelfth attribute, renaming one or
  changing a message key meant four edits, and no reader could tell a deliberate
  branch difference from an omission. The four call sites now go through one
  `cycle_start_form` define, with the three differences that track the branch
  each serves — the htmx target, the wrapper class and whether the button paints
  as the primary action — passed as parameters. A new barrier derives the
  expected attribute set from the confirm script itself and fails on any policy
  node that renders without all of it, so a partial copy can no longer look like
  a deliberate one. The rendered markup is unchanged apart from indentation and,
  on the dashboard, the order in which the form's own attributes appear.
