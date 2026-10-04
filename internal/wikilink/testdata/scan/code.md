Before the fence [[OTM-1 Before]].

```
[[OTM-90 Fenced]] is code.
```

~~~markdown
![[OTM-91 Tilde fenced]]
~~~

   ```go
indented up to three spaces still fences [[OTM-92 Indented fence]]
   ```

````
a longer fence is not closed by a shorter one
```
[[OTM-93 Still fenced]]
````

After the fences [[OTM-2 After]].

Inline `[[OTM-94 Inline code]]` is code, as is ``[[OTM-95 Double `tick`]]``,
but a lone ` backtick is text, so [[OTM-3 After a lone backtick]] links.

A span never crosses a blank line: `open

[[OTM-4 After a blank line]] and close`.

A span that starts inside a link breaks it: [[OTM-96 `split]]` here.

Mismatched runs: ``[[OTM-5 Between mismatched runs]]` stays text.

``` not a fence when the info string has a backtick ` [[OTM-6 Not fenced]]

Indented code after a blank line:

    [[OTM-80 Indented code]] is code,

    and stays code across a blank line [[OTM-81 Still indented]].
	[[OTM-82 Tab indented]] is code too.
Unindented again, [[OTM-7 After indented code]] links.
A paragraph continues onto an indented line
    [[OTM-8 Paragraph continuation]], which links.

- A list item's content

    [[OTM-9 List content]] is indented, but links.
  - nested

        [[OTM-10 Nested list content]] links too.

~~~
an unclosed fence runs to the end [[OTM-97 Unclosed]]
