INSERT INTO review_prompt_revisions(scope,repository,text,hash,origin,author,created_at)
VALUES(
  'validation',
  '',
  'High-signal validation

Act as an independent verifier. Evaluate every candidate finding against the supplied diff and applicable rules.

Approve a candidate only when the problem is concrete, introduced by this pull request, actionable, non-duplicative, and supported by the available evidence. For catalog findings, confirm that the cited rule applies to the changed file and behavior. Reject speculation, style preferences, generic test requests, issues requiring unseen context, pre-existing problems, and findings below the confidence threshold defined by the review policy.

Do not create, rewrite, reformat, or expand findings. Return only the candidate IDs that pass validation.',
  'ca5f62ad27c6a8f5aae6077aa13f453c76ca78d51ab3d72f58c69d1200f61791',
  'product',
  'system',
  strftime('%Y-%m-%dT%H:%M:%SZ','now')
);

INSERT INTO review_prompt_bindings(scope,repository,revision_id,use_global,version)
VALUES('validation','',last_insert_rowid(),0,1);
