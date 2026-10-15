package store

// ShortlistFirst selects the first window.
const ShortlistFirst = `SELECT a.candidate FROM assessments AS a CROSS JOIN candidates AS c ON c.id=a.candidate WHERE c.status='active' AND a.excluded=0 AND (?1 IS NULL OR a.minutes<=?1) AND (?2 IS NULL OR a.revenue_low>=?2) AND (?3='' OR c.class=?3) AND (?4='' OR EXISTS (SELECT 1 FROM candidate_cargo AS k WHERE k.candidate=c.id AND k.cargo=?4)) ORDER BY a.operating_key DESC,a.candidate ASC LIMIT ?5`

// ShortlistAfter selects the window after a position.
const ShortlistAfter = `SELECT a.candidate FROM assessments AS a CROSS JOIN candidates AS c ON c.id=a.candidate WHERE c.status='active' AND a.excluded=0 AND (?1 IS NULL OR a.minutes<=?1) AND (?2 IS NULL OR a.revenue_low>=?2) AND (?3='' OR c.class=?3) AND (?4='' OR EXISTS (SELECT 1 FROM candidate_cargo AS k WHERE k.candidate=c.id AND k.cargo=?4)) AND a.operating_key<=?5 AND (a.operating_key<?5 OR a.candidate>?6) ORDER BY a.operating_key DESC,a.candidate ASC LIMIT ?7`

// ShortlistBefore counts eligible entries through a position.
const ShortlistBefore = `SELECT count(*) FROM assessments AS a CROSS JOIN candidates AS c ON c.id=a.candidate WHERE c.status='active' AND a.excluded=0 AND (?1 IS NULL OR a.minutes<=?1) AND (?2 IS NULL OR a.revenue_low>=?2) AND (?3='' OR c.class=?3) AND (?4='' OR EXISTS (SELECT 1 FROM candidate_cargo AS k WHERE k.candidate=c.id AND k.cargo=?4)) AND a.operating_key>=?5 AND (a.operating_key>?5 OR a.candidate<=?6)`

// CandidatesFirst selects the first window.
const CandidatesFirst = `SELECT id,status,merged_into,name,names,street,city,state,zip,county,phone,email,people,class,cargo,created,updated FROM candidates WHERE status='active' AND (?1='' OR instr(names_lower,?1)>0) AND (?2='' OR zip=?2) AND (?3='' OR EXISTS (SELECT 1 FROM links AS l WHERE l.candidate=candidates.id AND l.phone_key=?3)) ORDER BY created DESC,id DESC LIMIT ?4`

// CandidatesAfter selects the window after a position.
const CandidatesAfter = `SELECT id,status,merged_into,name,names,street,city,state,zip,county,phone,email,people,class,cargo,created,updated FROM candidates WHERE status='active' AND (?1='' OR instr(names_lower,?1)>0) AND (?2='' OR zip=?2) AND (?3='' OR EXISTS (SELECT 1 FROM links AS l WHERE l.candidate=candidates.id AND l.phone_key=?3)) AND (created,id)<(?4,?5) ORDER BY created DESC,id DESC LIMIT ?6`

// StartedFirst selects the first window.
const StartedFirst = `SELECT id,kind,origin,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery FROM passes ORDER BY started DESC,id DESC LIMIT ?1`

// StartedAfter selects the window after a position.
const StartedAfter = `SELECT id,kind,origin,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery FROM passes WHERE (started,id)<(?1,?2) ORDER BY started DESC,id DESC LIMIT ?3`

// ShortlistLast selects the reversed window from the end.
const ShortlistLast = `SELECT a.candidate FROM assessments AS a CROSS JOIN candidates AS c ON c.id=a.candidate WHERE c.status='active' AND a.excluded=0 AND (?1 IS NULL OR a.minutes<=?1) AND (?2 IS NULL OR a.revenue_low>=?2) AND (?3='' OR c.class=?3) AND (?4='' OR EXISTS (SELECT 1 FROM candidate_cargo AS k WHERE k.candidate=c.id AND k.cargo=?4)) ORDER BY a.operating_key ASC,a.candidate DESC LIMIT ?5`

// ShortlistPreceding selects the reversed window preceding a position.
const ShortlistPreceding = `SELECT a.candidate FROM assessments AS a CROSS JOIN candidates AS c ON c.id=a.candidate WHERE c.status='active' AND a.excluded=0 AND (?1 IS NULL OR a.minutes<=?1) AND (?2 IS NULL OR a.revenue_low>=?2) AND (?3='' OR c.class=?3) AND (?4='' OR EXISTS (SELECT 1 FROM candidate_cargo AS k WHERE k.candidate=c.id AND k.cargo=?4)) AND a.operating_key>=?5 AND (a.operating_key>?5 OR a.candidate<?6) ORDER BY a.operating_key ASC,a.candidate DESC LIMIT ?7`

// StartedLast selects the reversed window from the end.
const StartedLast = `SELECT id,kind,origin,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery FROM passes ORDER BY started ASC,id ASC LIMIT ?1`

// StartedPreceding selects the reversed window preceding a position.
const StartedPreceding = `SELECT id,kind,origin,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery FROM passes WHERE (started,id)>(?1,?2) ORDER BY started ASC,id ASC LIMIT ?3`
