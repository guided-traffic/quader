import { MAX_HIGHLIGHT, highlightYaml } from './yaml-highlight';

const kinds = (text: string) => highlightYaml(text).map((l) => l.filter((t) => t.text.trim() !== '').map((t) => `${t.kind}:${t.text.trim()}`));

describe('YAML highlighting (R-ANS-050)', () => {
  const example = [
    '# Mount the data disk',
    '- name: Mount up device by label',
    '  ansible.posix.mount:',
    '    path: /srv/disk',
    "    src: 'LABEL=SOME_LABEL'",
    '    fstype: ext4',
    '    passno: 2',
    '    backup: true  # keep a copy',
    '    opts: "{{ mount_opts | default(\'rw\') }}"',
    '    boot: no',
    '    state: ~',
    '  when: "{{ disk_present }}" and x',
    '  loop: [a, "b", 3]',
    '  vars: {mode: 0644, keep: false}',
    '- ansible.builtin.shell: |',
    '    echo key: value {{ item }}',
    '',
    '    # not a comment',
    '  register: out',
    '---',
  ].join('\n');

  it('R-ANS-050 the tokens of a line join to the line, character for character', () => {
    const lines = highlightYaml(example + '\n');
    expect(lines.map((l) => l.map((t) => t.text).join('')).join('\n')).toBe(example);
  });

  it('R-ANS-050 keys, strings, numbers, booleans, null, comments, punctuation and Jinja have kinds of their own', () => {
    const k = kinds(example);
    expect(k[0]).toEqual(['comment:# Mount the data disk']);
    expect(k[1]).toEqual(['punct:-', 'key:name', 'punct::', 'plain:Mount up device by label']);
    expect(k[2]).toEqual(['key:ansible.posix.mount', 'punct::']);
    expect(k[4]).toEqual(['key:src', 'punct::', "string:'LABEL=SOME_LABEL'"]);
    expect(k[6]).toEqual(['key:passno', 'punct::', 'number:2']);
    expect(k[7]).toEqual(['key:backup', 'punct::', 'bool:true', 'comment:# keep a copy']);
    expect(k[8]).toEqual(['key:opts', 'punct::', 'string:"', "jinja:{{ mount_opts | default('rw') }}", 'string:"']);
    expect(k[9]).toEqual(['key:boot', 'punct::', 'bool:no']);
    expect(k[10]).toEqual(['key:state', 'punct::', 'bool:~']);
    expect(k[11]).toEqual(['key:when', 'punct::', 'string:"', 'jinja:{{ disk_present }}', 'string:"', 'plain:and x']);
    expect(k[12]).toEqual(['key:loop', 'punct::', 'punct:[', 'plain:a', 'punct:,', 'string:"b"', 'punct:,', 'number:3', 'punct:]']);
    expect(k[13]).toEqual(['key:vars', 'punct::', 'punct:{', 'key:mode', 'punct::', 'number:0644', 'punct:,', 'key:keep', 'punct::', 'bool:false', 'punct:}']);
    expect(k[19]).toEqual(['punct:---']);
  });

  it('R-ANS-050 the lines of a block scalar are its text, with the Jinja inside, until a line is indented no deeper than its key', () => {
    const k = kinds(example);
    expect(k[14]).toEqual(['punct:-', 'key:ansible.builtin.shell', 'punct::', 'punct:|']);
    expect(k[15]).toEqual(['string:echo key: value', 'jinja:{{ item }}']);
    expect(k[16]).toEqual([]);
    expect(k[17]).toEqual(['string:# not a comment']);
    expect(k[18]).toEqual(['key:register', 'punct::', 'plain:out']);
  });

  it('R-ANS-050 a sample written as JSON reads its keys and values', () => {
    expect(kinds('{\n  "name": "sda1",\n  "size": 1024,\n  "ok": true\n}')).toEqual([
      ['punct:{'],
      ['key:"name"', 'punct::', 'string:"sda1"', 'punct:,'],
      ['key:"size"', 'punct::', 'number:1024', 'punct:,'],
      ['key:"ok"', 'punct::', 'bool:true'],
      ['punct:}'],
    ]);
  });

  it('R-ANS-050 markup in an example stays text, and a text past the bound is plain', () => {
    expect(kinds('- msg: <script>alert(1)</script>')).toEqual([['punct:-', 'key:msg', 'punct::', 'plain:<script>alert(1)</script>']]);
    const huge = 'a: b\n'.repeat(MAX_HIGHLIGHT / 4);
    expect(highlightYaml(huge)[0]).toEqual([{ text: 'a: b', kind: 'plain' }]);
  });
});
