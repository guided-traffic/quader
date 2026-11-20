import { KEEP_MAX, breakPieces, commandLines, highlight, isAddress, keepsTogether, overlay } from './code-syntax';

const kinds = (content: string, language: Parameters<typeof highlight>[1], marks: Parameters<typeof highlight>[2] = []) =>
  highlight(content, language, marks).map((l) => l.segments.filter((s) => s.text.trim() !== '').map((s) => `${s.kind}:${s.text}`));

describe('code syntax (R-UI-121, Q-UI-801)', () => {
  it('R-UI-121 a line is the snippet\'s text, piece for piece: the segments join to it, in every language', () => {
    const samples: [string, Parameters<typeof highlight>[1]][] = [
      ['registry=https://q/api/npm/w/r/\n//q/api/npm/w/r/:_authToken=${QUADER_TOKEN}\n', 'ini'],
      ['npmScopes:\n  acme:\n    npmRegistryServer: "https://q/"\n# note\n', 'yaml'],
      ['<?xml version="1.0"?>\n<add key="quader" value="https://q/v3/index.json" />\n', 'xml'],
      ['{"auths": {"q": {"password": "x"}}}', 'json'],
      ["printf '%s' \"${QUADER_TOKEN}\" | docker login q/w/r --username quader --password-stdin\n", 'shell'],
      ['nuget sources add -Name quader ^\n  -Password %QUADER_TOKEN%\n', 'cmd'],
      ['$env:X = "$env:QUADER_TOKEN"\n', 'powershell'],
    ];
    for (const [content, language] of samples) {
      const lines = highlight(content, language, [{ text: '${QUADER_TOKEN}', kind: 'token' }]);
      expect(lines.map((l) => l.segments.map((s) => s.text).join('')).join('\n'), language).toBe(content.replace(/\n$/, ''));
    }
  });

  it('R-UI-121 a command colours its command, flags, strings and addresses; a file its keys and values', () => {
    expect(kinds('npm publish --registry https://q/api/npm/w/r/\n', 'shell')).toEqual([
      ['command:npm', 'plain:publish', 'flag:--registry', 'url:https://q/api/npm/w/r/'],
    ]);
    expect(kinds('registry=https://q/api/npm/w/r/\n', 'ini')).toEqual([['key:registry', 'punct:=', 'url:https://q/api/npm/w/r/']]);
    expect(kinds('  - name: <package>\n', 'yaml', [{ text: '<package>', kind: 'value' }])).toEqual([['punct:- ', 'key:name', 'punct::', 'value:<package>']]);
    expect(kinds('<add key="quader" />\n', 'xml')).toEqual([['punct:<', 'command:add', 'key:key', 'punct:=', 'string:"quader"', 'punct:/>']]);
    expect(kinds('$env:X = "y"\n', 'powershell')[0]?.[0]).toBe('variable:$env:X');
    expect(kinds('set X=%QUADER_TOKEN%\n', 'cmd')).toEqual([['command:set', 'variable:X', 'punct:=', 'variable:%QUADER_TOKEN%']]);
  });

  it('R-UI-121 the marks the server names are laid over the colours: the token\'s reference and the reader\'s values, wherever they stand', () => {
    const lines = highlight("docker tag myapp:1.0 q/w/r/myapp:1.0\nprintf '%s' \"${QUADER_TOKEN}\" | x\n", 'shell', [
      { text: 'myapp:1.0', kind: 'value' },
      { text: '${QUADER_TOKEN}', kind: 'token' },
    ]);
    expect(lines[0]!.segments.filter((s) => s.kind === 'value').map((s) => s.text)).toEqual(['myapp:1.0', 'myapp:1.0']);
    // Inside a quoted string the reference is one mark, the quotes stay strings.
    expect(lines[1]!.segments.filter((s) => s.text.trim() !== '').map((s) => `${s.kind}:${s.text}`)).toEqual([
      'command:printf',
      "string:'%s'",
      'string:"',
      'token:${QUADER_TOKEN}',
      'string:"',
      'punct:|',
      'command:x',
    ]);
    // A mark inside a longer one does not cut it.
    expect(overlay([{ text: 'a <package>.tgz b', kind: 'plain', keep: false, pieces: [] }], [
      { text: '<package>', kind: 'value' },
      { text: '<package>.tgz', kind: 'value' },
    ]).map((s) => s.text)).toEqual(['a ', '<package>.tgz', ' b']);
  });

  it('R-UI-121 a terminal prompts a command once: not on a continued line, a here-document, a block or a comment', () => {
    const prompts = (content: string, language: 'shell' | 'powershell' | 'cmd') => commandLines(content.split('\n'), language).map((l) => l.prompt);
    expect(prompts('a \\\n  --b\nc', 'shell')).toEqual(['$', '', '$']);
    // After a pipe that ends a line, the continued line starts with a command.
    const piped = highlight("printf 'x' | \\\n  kubectl create secret\n", 'shell')[1]!.segments.filter((s) => s.text.trim() !== '');
    expect(piped[0]).toMatchObject({ kind: 'command', text: 'kubectl' });
    expect(prompts('f() {\n  curl - <<EOF\nheader = "x"\nEOF\n}\nf --fail', 'shell')).toEqual(['$', '', '', '', '', '$']);
    expect(prompts('# only a comment', 'shell')).toEqual(['']);
    expect(prompts('a `\n  -b', 'powershell')).toEqual(['PS>', '']);
    expect(prompts('a ^\n  -b\nREM note', 'cmd')).toEqual(['>', '', '']);
    expect(highlight('x', 'ini')[0]!.prompt).toBe('');
  });

  it('R-UI-125 the wrap rule: a flag, a mark and a token\'s reference never break inside; an address breaks after its slashes', () => {
    expect(keepsTogether('flag', '--password-stdin')).toBe(true);
    expect(keepsTogether('value', '<package>')).toBe(true);
    expect(keepsTogether('token', '${QUADER_TOKEN}')).toBe(true);
    expect(keepsTogether('url', 'https://q/')).toBe(false);
    // A word stays whole; the line breaks at its spaces.
    expect(keepsTogether('plain', '.dockerconfigjson=/dev/stdin')).toBe(true);
    expect(keepsTogether('plain', ' ')).toBe(false);
    expect(keepsTogether('string', "'%s'")).toBe(false);
    // A token written into the snippet (57 characters) may break, or it would push the window past a phone's width.
    expect(keepsTogether('token', 'qdr_pat_' + 'x'.repeat(49))).toBe(false);
    expect(keepsTogether('flag', '-'.repeat(KEEP_MAX + 1))).toBe(false);
    expect(breakPieces('url', 'https://q/api/npm/web-frontend/npm-internal/')).toEqual(['https:/', '/', 'q/', 'api/', 'npm/', 'web-frontend/', 'npm-internal/']);
    expect(breakPieces('url', "'https://q/upload/a.deb?d=s&c=m'")).toEqual(["'https:/", '/', 'q/', 'upload/', 'a.deb?', 'd=s&', "c=m'"]);
    expect(breakPieces('flag', '--tag')).toEqual(['--tag']);
    // The flag after a space is one segment: the line breaks before "--", never after it.
    const tag = highlight('docker buildx build --platform linux/amd64 --tag q/w/r/myapp:1.0 --push .\n', 'shell')[0]!.segments;
    expect(tag.filter((s) => s.kind === 'flag').map((s) => [s.text, s.keep])).toEqual([
      ['--platform', true],
      ['--tag', true],
      ['--push', true],
    ]);
  });

  it('R-UI-121 an address is a scheme, a host with a port or a path', () => {
    expect(['https://q/', 'localhost:4200/w/r', '/etc/apt/keyrings', './a.tgz', '~/.netrc', 'quader.example.com/w/r'].every(isAddress)).toBe(true);
    expect(['quader', 'install', 'linux/amd64,linux/arm64'].some(isAddress)).toBe(false);
  });
});
