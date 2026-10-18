ALTER TABLE rules ADD COLUMN rule_text TEXT NOT NULL DEFAULT '';
ALTER TABLE rules ADD COLUMN exception_text TEXT NOT NULL DEFAULT '';
ALTER TABLE rules ADD COLUMN bad_example TEXT NOT NULL DEFAULT '';
ALTER TABLE rules ADD COLUMN good_example TEXT NOT NULL DEFAULT '';

UPDATE rules SET rule_text = body WHERE rule_text = '';

UPDATE rules SET scope = trim(trim(
  CASE
    WHEN instr(substr(body, instr(body, '- **Scope:**') + 12), '- **') > 0
      THEN substr(substr(body, instr(body, '- **Scope:**') + 12), 1, instr(substr(body, instr(body, '- **Scope:**') + 12), '- **') - 1)
    ELSE substr(body, instr(body, '- **Scope:**') + 12)
  END, char(10) || char(13) || ' '
), '`') WHERE instr(body, '- **Scope:**') > 0;

UPDATE rules SET rule_text = trim(
  CASE
    WHEN instr(substr(body, instr(body, '- **Rule:**') + 12), '- **') > 0
      THEN substr(substr(body, instr(body, '- **Rule:**') + 12), 1, instr(substr(body, instr(body, '- **Rule:**') + 12), '- **') - 1)
    ELSE substr(body, instr(body, '- **Rule:**') + 12)
  END, char(10) || char(13) || ' '
) WHERE instr(body, '- **Rule:**') > 0;

UPDATE rules SET exception_text = trim(
  CASE
    WHEN instr(substr(body, instr(body, '- **Exception:**') + 17), '- **') > 0
      THEN substr(substr(body, instr(body, '- **Exception:**') + 17), 1, instr(substr(body, instr(body, '- **Exception:**') + 17), '- **') - 1)
    ELSE substr(body, instr(body, '- **Exception:**') + 17)
  END, char(10) || char(13) || ' '
) WHERE instr(body, '- **Exception:**') > 0;

UPDATE rules SET bad_example = trim(
  CASE
    WHEN instr(substr(body, instr(body, '- **Bad:**') + 11), '- **') > 0
      THEN substr(substr(body, instr(body, '- **Bad:**') + 11), 1, instr(substr(body, instr(body, '- **Bad:**') + 11), '- **') - 1)
    ELSE substr(body, instr(body, '- **Bad:**') + 11)
  END, char(10) || char(13) || ' '
) WHERE instr(body, '- **Bad:**') > 0;

UPDATE rules SET good_example = trim(
  CASE
    WHEN instr(substr(body, instr(body, '- **Good:**') + 12), '- **') > 0
      THEN substr(substr(body, instr(body, '- **Good:**') + 12), 1, instr(substr(body, instr(body, '- **Good:**') + 12), '- **') - 1)
    ELSE substr(body, instr(body, '- **Good:**') + 12)
  END, char(10) || char(13) || ' '
) WHERE instr(body, '- **Good:**') > 0;

ALTER TABLE review_rule_inputs ADD COLUMN rule_text TEXT NOT NULL DEFAULT '';
ALTER TABLE review_rule_inputs ADD COLUMN exception_text TEXT NOT NULL DEFAULT '';
ALTER TABLE review_rule_inputs ADD COLUMN bad_example TEXT NOT NULL DEFAULT '';
ALTER TABLE review_rule_inputs ADD COLUMN good_example TEXT NOT NULL DEFAULT '';
