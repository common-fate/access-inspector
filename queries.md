Testing some SQLite statements:

```sql
SELECT
    accountassignment.account,
    accountassignment.permission_set as permission_set_arn,
    permissionset.name as permission_set_name,
    user.email,
    groupmembership.*
FROM accountassignment
INNER JOIN groupmembership ON accountassignment."group" = groupmembership."group"
INNER JOIN user ON accountassignment.user = user.id
INNER JOIN permissionset ON accountassignment.permission_set = permissionset.id
```

```sql
SELECT
    accountassignment.id,
    accountassignment.account,
    account.name as account_name,
    accountassignment.permission_set as permission_set_arn,
    "group".name as group_name,
    user.email
FROM accountassignment
INNER JOIN account ON accountassignment.account = account.id
INNER JOIN groupmembership ON accountassignment."group" = groupmembership."group"
INNER JOIN user ON groupmembership."user" = user.id
INNER JOIN "group" ON groupmembership."group" = "group".id
```
