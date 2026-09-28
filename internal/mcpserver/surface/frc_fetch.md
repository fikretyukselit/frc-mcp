Fetch the full text of a documentation section by the id returned from frc_search (or a frc://chunk/<id> URI).

Use when: a search snippet is not enough and you need the complete section, including all code samples.
Don't use when: you have no id yet — call frc_search first. For API signatures call frc_api.

Long sections are paged: pass next_cursor back as cursor. Example:
{"id": "phoenix6/2026/motion-magic#0"}
