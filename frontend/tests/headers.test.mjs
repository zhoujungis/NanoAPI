import test from 'node:test';
import assert from 'node:assert/strict';

import { filterHeaders, formatHeaders } from '../dist/js/headers.js';

test('formats repeated header values on separate lines', () => {
  assert.equal(
    formatHeaders({
      'Content-Type': ['application/json; charset=utf-8'],
      'Set-Cookie': ['sid=abc; Path=/', 'theme=dark; Path=/'],
    }),
    'Content-Type: application/json; charset=utf-8\nSet-Cookie: sid=abc; Path=/\nSet-Cookie: theme=dark; Path=/'
  );
});

test('handles missing headers and scalar values', () => {
  assert.equal(formatHeaders(null), '');
  assert.equal(formatHeaders(undefined), '');
  assert.equal(formatHeaders({}), '');
  assert.equal(
    formatHeaders({ 'X-Empty': [''], 'X-Scalar': 'v', 'X-Number': [0] }),
    'X-Empty: \nX-Scalar: v\nX-Number: 0'
  );
});

test('skips null values and empty arrays', () => {
  assert.equal(
    formatHeaders({ 'X-Null': [null, undefined], 'X-Skip': [], 'X-Ok': ['1'] }),
    'X-Ok: 1'
  );
});

test('preserves header order', () => {
  assert.equal(
    formatHeaders({ B: ['2'], A: ['1'], C: ['3'] }),
    'B: 2\nA: 1\nC: 3'
  );
});

test('filters headers by name or value, case-insensitively', () => {
  const headers = {
    'Content-Type': ['application/json'],
    'Set-Cookie': ['sid=abc; Path=/', 'theme=dark; Path=/'],
    'X-Request-Id': ['mock-123'],
  };

  assert.deepEqual(filterHeaders(headers, 'CONTENT'), [['Content-Type', ['application/json']]]);
  assert.deepEqual(filterHeaders(headers, 'abc'), [
    ['Set-Cookie', ['sid=abc; Path=/', 'theme=dark; Path=/']],
  ]);
  assert.deepEqual(filterHeaders(headers, 'cookie').map(([key]) => key), ['Set-Cookie']);
  assert.deepEqual(filterHeaders(headers, 'zzz'), []);
});

test('empty query returns all headers and keeps order', () => {
  const headers = { B: ['2'], A: ['1'] };
  assert.deepEqual(filterHeaders(headers, ''), [
    ['B', ['2']],
    ['A', ['1']],
  ]);
  assert.deepEqual(filterHeaders(headers, '   '), [
    ['B', ['2']],
    ['A', ['1']],
  ]);
  assert.deepEqual(filterHeaders(null, 'x'), []);
});
