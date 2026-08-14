none

Dead-code removal with no user-visible effect: three template functions no
template calls (`symptomGroup`, `roleLabel`, `statusOK`) and everything only
they reached — the symptom-group and role-label policies, the builtin symptom
catalogue's unread `Group` field, the non-dismissible status-ok markup helpers,
and the six locale keys those helpers translated. The dismissible status-ok
wrapper, which handlers do use, is untouched.
