import test from 'node:test';
import assert from 'node:assert/strict';

import { formatEnvValues, parseEnvText } from '../dist/js/envbulk.js';

test('parses basic lines, comments, blanks and export prefix', () => {
  const { rows, ignored } = parseEnvText(
    '# comment\n\nbaseUrl=https://api.example.com\ntoken = abc\nexport extra=1\n'
  );
  assert.equal(ignored, 0);
  assert.deepEqual(
    rows.map((row) => [row.key, row.value]),
    [
      ['baseUrl', 'https://api.example.com'],
      ['token', 'abc'],
      ['extra', '1'],
    ]
  );
  assert.ok(rows.every((row) => row.enabled));
});

test('keeps enabled state and description for existing keys', () => {
  const existing = [
    { key: 'baseUrl', value: 'old', enabled: false, desc: '本地地址' },
    { key: 'token', value: 'old', enabled: true, desc: '令牌' },
  ];
  const { rows } = parseEnvText('baseUrl=https://new.example.com\ntoken=new', existing);
  assert.equal(rows[0].value, 'https://new.example.com');
  assert.equal(rows[0].enabled, false);
  assert.equal(rows[0].desc, '本地地址');
  assert.equal(rows[1].desc, '令牌');
});

test('handles quoted and escaped values', () => {
  const { rows } = parseEnvText(
    [
      'spaces="  padded  "',
      "single='literal value'",
      'equals="a=b"',
      String.raw`multiline="line1\nline2"`,
      String.raw`quoted="say \"hi\""`,
    ].join('\n')
  );
  assert.equal(rows[0].value, '  padded  ');
  assert.equal(rows[1].value, 'literal value');
  assert.equal(rows[2].value, 'a=b');
  assert.equal(rows[3].value, 'line1\nline2');
  assert.equal(rows[4].value, 'say "hi"');
});

test('last duplicate key wins', () => {
  const { rows } = parseEnvText('a=1\nb=2\na=3');
  assert.deepEqual(
    rows.map((row) => [row.key, row.value]),
    [
      ['a', '3'],
      ['b', '2'],
    ]
  );
});

test('counts invalid lines and keeps empty values', () => {
  const { rows, ignored } = parseEnvText('noequals\n=value\nKEY=\n   \n# note');
  assert.equal(ignored, 2);
  assert.deepEqual(
    rows.map((row) => [row.key, row.value]),
    [['KEY', '']]
  );
});

test('format and parse round-trips tricky values', () => {
  const values = [
    { key: 'plain', value: 'abc', enabled: true, desc: '' },
    { key: 'spaces', value: '  padded  ', enabled: true, desc: '' },
    { key: 'quote', value: 'say "hi"', enabled: true, desc: '' },
    { key: 'single', value: "'wrapped'", enabled: true, desc: '' },
    { key: 'path', value: String.raw`C:\temp\file.txt`, enabled: true, desc: '' },
    { key: 'multi', value: 'line1\nline2', enabled: false, desc: '' },
  ];
  const text = formatEnvValues(values);
  const { rows } = parseEnvText(text, values);
  assert.deepEqual(
    rows.map((row) => [row.key, row.value, row.enabled]),
    values.map((row) => [row.key, row.value, row.enabled])
  );
});

test('format omits blank keys and supports empty values', () => {
  const text = formatEnvValues([
    { key: '', value: 'x', enabled: true, desc: '' },
    { key: 'empty', value: '', enabled: true, desc: '' },
  ]);
  assert.equal(text, 'empty=');
});
