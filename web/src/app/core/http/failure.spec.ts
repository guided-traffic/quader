import { HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { ApiError } from '../../api/problem';
import { apiErrorOf, asFailure, redactUrl, shownHeaders } from './failure';

describe('a failed answer as an ApiError (R-UI-216)', () => {
  it('R-UI-216 names a credential parameter without its value and leaves the rest of the address as it is', () => {
    expect(redactUrl('/api/v1/x?page=2&token=abc&X-Amz-Signature=5e3d&q=a%20b#top')).toBe(
      '/api/v1/x?page=2&token=[redacted]&X-Amz-Signature=[redacted]&q=a%20b#top',
    );
    expect(redactUrl('/api/v1/x?access_token=1&api_key=2&sig=3&password=4&state=inactive')).toBe(
      '/api/v1/x?access_token=[redacted]&api_key=[redacted]&sig=[redacted]&password=[redacted]&state=inactive',
    );
    expect(redactUrl('/api/v1/x')).toBe('/api/v1/x');
  });

  it('R-UI-216 shows every header a script reads but those that carry a credential; an address header is redacted', () => {
    const headers = new HttpHeaders({
      'Content-Type': 'text/plain',
      'Set-Cookie': '__Host-qdr_session=1',
      Authorization: 'Bearer x',
      Location: 'https://s3.example.com/b?X-Amz-Credential=a&X-Amz-Signature=b',
    });
    expect(shownHeaders(headers)).toEqual([
      ['content-type', 'text/plain'],
      ['location', 'https://s3.example.com/b?X-Amz-Credential=[redacted]&X-Amz-Signature=[redacted]'],
    ]);
  });

  it('R-UI-216 a successful answer that cannot be read is said as such and keeps no body, which may carry a secret', async () => {
    const err = new HttpErrorResponse({
      status: 200,
      error: { error: new SyntaxError('Unexpected end of JSON input'), text: '{"secret":"qdr_abc"' },
      headers: new HttpHeaders({ 'Content-Type': 'application/json' }),
    });
    const e = await apiErrorOf({ method: 'POST', urlWithParams: '/api/v1/tokens' }, err, { retried: false, inRow: 1 });
    expect(e.answer.unreadable).toBe(true);
    expect(e.answer.body).toBeNull();
    expect(e.answer.fault).toBe('SyntaxError: Unexpected end of JSON input');
    expect(JSON.stringify(e.answer)).not.toContain('qdr_abc');
  });

  it('R-UI-216 a JSON answer that is no problem document is kept as text; a problem in a text body is read as one', async () => {
    const other = new HttpErrorResponse({ status: 500, error: { message: 'panic', trace: ['a'] } });
    const e = await apiErrorOf({ method: 'GET', urlWithParams: '/api/v1/x' }, other, { retried: true, inRow: 3 });
    expect(e.problem).toBeNull();
    expect(e.answer.body).toContain('"message": "panic"');
    expect(e.afterRetry).toBe(true);
    expect(e.answer.inRow).toBe(3);
    const text = new HttpErrorResponse({ status: 409, error: '{"code":"conflict","requestId":"rid-t"}' });
    const t = await apiErrorOf({ method: 'GET', urlWithParams: '/api/v1/x' }, text, { retried: false, inRow: 1 });
    expect(t.code).toBe('conflict');
    expect(t.requestId).toBe('rid-t');
    expect(t.answer.body).toBeNull();
  });

  it('R-UI-216 a failure of the page itself is no lost connection: it keeps its message and has no call', () => {
    const e = asFailure(new TypeError('x is undefined'));
    expect(e.status).toBe(0);
    expect(e.answer.fault).toBe('TypeError: x is undefined');
    expect(e.answer.call).toBeNull();
    const api = new ApiError(404, { code: 'not_found' }, null);
    expect(asFailure(api)).toBe(api);
  });
});
