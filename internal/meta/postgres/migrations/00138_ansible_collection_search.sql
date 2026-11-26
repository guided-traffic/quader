-- quader:phase expand
-- quader:no-transaction
--
-- The search of Ansible Galaxy collections (formats/ansible.md R-ANS-057, Q-ANS-120, the measurement in docs/developer/packages.md):
-- the full-text vector of a collection version's namespace and name (weight A), its tags (B) and its description (C), in the
-- configuration of the documentation search ('simple'), as an expression index, so that the package list's find reads the matching rows
-- through the index instead of building the vector of every version of the repositories it reads (447 ms with 50,000 collections before
-- this step). The statement AnsibleViewFind repeats the expression word for word: the planner uses the index only for the same
-- expression. Built without blocking writes; no row is written.
CREATE INDEX CONCURRENTLY IF NOT EXISTS ansible_collection_versions_search ON ansible_collection_versions USING gin ((
    setweight(to_tsvector('simple'::regconfig, translate(name, '._', '  ')), 'A')
    || setweight(jsonb_to_tsvector('simple'::regconfig,
         CASE WHEN jsonb_typeof(collection_info -> 'tags') = 'array' THEN collection_info -> 'tags' ELSE '[]'::jsonb END, '["string"]'), 'B')
    || setweight(to_tsvector('simple'::regconfig, COALESCE(collection_info ->> 'description', '')), 'C')
));
