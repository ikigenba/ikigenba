UPDATE sites SET listed = CASE WHEN json_extract(payload,'$.Listed') THEN 1 ELSE 0 END;
