# Flow: <name>

**Goal**: <what the user is trying to accomplish>
**Frequency**: <core | common | rare>
**Trigger**: <what initiates this flow>

## Happy path

1. **Screen**: <name>
   **User does**: <action>
   **System shows**: <result>

2. **Screen**: <name>
   **User does**: <action>
   **System shows**: <result>

...

## Decision points

- **Step X**: if <condition>, go to <step Y>; otherwise continue to step <Z>.

## Cancel paths

- At step <N>, `esc` → returns to <screen>, no changes saved.
- At step <N>, `q` → ...

## Error paths

- If <thing fails>: show <error pattern>; offer <recovery>.
- ...

## Acceptance

The flow is complete when:
- [ ] <observable result 1>
- [ ] <observable result 2>
- [ ] <observable result 3>
