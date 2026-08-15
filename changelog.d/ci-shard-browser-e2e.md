none

CI only: the browser e2e suite runs as three shards on separate runners, with a
thin gate job keeping the required `e2e` status check. No product code, and no
change to how many tests share one database.
