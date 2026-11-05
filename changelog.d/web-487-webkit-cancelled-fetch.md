none

Test-suite change with no user-visible effect: the cross-browser settings
smoke waits for the export summary fetch to settle before it navigates away,
so WebKit no longer reports the cancelled fetch as a page error.
