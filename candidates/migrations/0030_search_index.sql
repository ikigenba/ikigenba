CREATE VIRTUAL TABLE search_index USING fts5(name, candidate UNINDEXED, display UNINDEXED, tokenize='trigram');
CREATE VIRTUAL TABLE search_index_vocab USING fts5vocab(search_index, 'row');
INSERT INTO search_index(name, candidate, display) SELECT name, id, 1 FROM candidates WHERE status = 'active' AND name <> '' UNION SELECT j.value, c.id, 0 FROM candidates AS c, json_each(c.names) AS j WHERE c.status = 'active' AND j.type = 'text' AND j.value <> '' AND j.value <> c.name;
