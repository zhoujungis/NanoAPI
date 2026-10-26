import test from 'node:test';
import assert from 'node:assert/strict';

import {
  extensionFor,
  filenameFromDisposition,
  headersFilename,
  safeFilename,
  selectionFilename,
  suggestResponseFilename,
} from '../dist/js/download.js';

test('infers extensions from content type', () => {
  assert.equal(extensionFor('application/json; charset=utf-8'), 'json');
  assert.equal(extensionFor('application/vnd.api+json'), 'json');
  assert.equal(extensionFor('application/atom+xml'), 'xml');
  assert.equal(extensionFor('text/plain; charset=gbk'), 'txt');
  assert.equal(extensionFor('image/png', true), 'png');
  assert.equal(extensionFor('application/octet-stream', true), 'bin');
  assert.equal(extensionFor('application/x-unknown', true), 'bin');
  assert.equal(extensionFor('application/x-unknown', false), 'txt');
  assert.equal(extensionFor('', true), 'bin');
  assert.equal(extensionFor('', false), 'txt');
});

test('reads filenames from content-disposition', () => {
  assert.equal(
    filenameFromDisposition({
      'content-disposition': ['attachment; filename="报告 2026.pdf"'],
    }),
    '报告 2026.pdf'
  );
  assert.equal(
    filenameFromDisposition({
      'Content-Disposition': ["attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt"],
    }),
    '报告.txt'
  );
  assert.equal(
    filenameFromDisposition({ 'content-disposition': ['inline; filename=plain.bin'] }),
    'plain.bin'
  );
  assert.equal(filenameFromDisposition({ 'content-type': ['application/json'] }), '');
  assert.equal(filenameFromDisposition(null), '');
});

test('sanitizes filenames and keeps extensions', () => {
  assert.equal(safeFilename('a:b*c?.json'), 'a_b_c_.json');
  assert.equal(safeFilename('C:\\tmp\\report.pdf'), 'report.pdf');
  assert.equal(safeFilename('folder/report.txt'), 'report.txt');
  assert.equal(safeFilename('  trailing.  '), 'trailing');
  assert.equal(safeFilename(' . ', 'response.txt'), 'response.txt');
  assert.equal(safeFilename('', 'response.bin'), 'response.bin');

  const long = safeFilename(`${'报'.repeat(130)}.json`);
  assert.ok(long.endsWith('.json'));
  assert.ok(Array.from(long).length <= 120);
});

test('uses the URL last segment and content type', () => {
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/users/42?verbose=1',
      contentType: 'application/json',
    }),
    '42.json'
  );
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/files/report.pdf',
      contentType: 'application/pdf',
      isBinary: true,
    }),
    'report.pdf'
  );
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/image',
      contentType: 'image/png',
      isBinary: true,
    }),
    'image.png'
  );
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/download/%E6%8A%A5%E5%91%8A',
      contentType: 'text/plain',
    }),
    '报告.txt'
  );
});

test('prefers content-disposition over the URL', () => {
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/download/123',
      contentType: 'application/octet-stream',
      isBinary: true,
      headers: { 'Content-Disposition': ['attachment; filename="real-file.bin"'] },
    }),
    'real-file.bin'
  );
  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/download/123',
      contentType: 'text/plain',
      headers: {
        'Content-Disposition': ["attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt"],
      },
    }),
    '报告.txt'
  );
});

test('builds selection filenames with txt extension', () => {
  assert.equal(selectionFilename('users.json'), 'users-selected.txt');
  assert.equal(selectionFilename('报告 2026.pdf'), '报告 2026-selected.txt');
  assert.equal(selectionFilename('C:\\tmp\\photo.png'), 'photo-selected.txt');
  assert.equal(selectionFilename('report'), 'report-selected.txt');
  assert.equal(selectionFilename(''), 'response-selected.txt');
  const long = selectionFilename(`${'报'.repeat(130)}.json`);
  assert.ok(long.endsWith('-selected.txt'));
  assert.ok(Array.from(long).length <= 120);
});

test('builds header filenames with txt extension', () => {
  assert.equal(headersFilename('users.json'), 'users-headers.txt');
  assert.equal(headersFilename('报告 2026.pdf'), '报告 2026-headers.txt');
  assert.equal(headersFilename('C:\\tmp\\photo.png'), 'photo-headers.txt');
  assert.equal(headersFilename('report'), 'report-headers.txt');
  assert.equal(headersFilename(''), 'response-headers.txt');
  const long = headersFilename(`${'报'.repeat(130)}.json`);
  assert.ok(long.endsWith('-headers.txt'));
  assert.ok(Array.from(long).length <= 120);
});

test('falls back to request name and generic names', () => {
  assert.equal(
    suggestResponseFilename(
      { url: 'https://api.example.com/', contentType: 'application/json' },
      { name: '获取用户列表' }
    ),
    '获取用户列表.json'
  );
  assert.equal(
    suggestResponseFilename(
      { url: '', contentType: 'text/plain' },
      { name: '接口' }
    ),
    '接口.txt'
  );
  assert.equal(
    suggestResponseFilename({ url: '', contentType: 'application/json' }, { name: '' }),
    'response.json'
  );
  assert.equal(
    suggestResponseFilename({ url: '', contentType: '', isBinary: true }, null),
    'response.bin'
  );
});

test('handles template URLs and bad filenames', () => {
  const fromTemplate = suggestResponseFilename(
    { url: '{{baseUrl}}/users', contentType: 'application/json' },
    { name: '用户' }
  );
  assert.ok(fromTemplate === 'users.json' || fromTemplate === '用户.json');

  assert.equal(
    suggestResponseFilename({
      url: 'https://api.example.com/data',
      contentType: 'application/json',
      headers: { 'content-disposition': ['attachment; filename="C:\\\\tmp\\\\a:b?.json"'] },
    }),
    'a_b_.json'
  );
});
