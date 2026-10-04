---
id: RET-3
title: Links in code
aliases:
  - Links in code
kind: issue
status: open
author: talvor
parent: null
blocked_by: []
labels: []
assignee: null
created: 2026-01-01T10:00:00Z
updated: 2026-01-01T10:00:00Z
---
Inline code `[[RET-1 Old title]]` and ``code with ` and [[RET-1 Old title]]`` stay.

```markdown
A fenced [[RET-1 Old title]] stays.
```

~~~
A tilde fence [[RET-1 Old title|too]] stays.
~~~

An indented code block:

    An indented [[RET-1 Old title]] stays.

- In a list, an indented line

    [[RET-1 Old title|listed]] is content, rewritten.

After the code, [[RET-1 Old title]] is rewritten.

<!-- otman:comments -->
## Comments
