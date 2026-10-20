<?php

namespace Magne4000\EasyPz\Http\Controllers;

use App\Models\Server;
use GuzzleHttp\Client;
use GuzzleHttp\Exception\GuzzleException;
use Illuminate\Contracts\Cache\Lock;
use Illuminate\Http\Request;
use Illuminate\Routing\Controller;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Log;
use Magne4000\EasyPz\Models\EasyPzServer;
use Magne4000\EasyPz\Services\EasyPzService;
use Magne4000\EasyPz\Support\DownloadSlots;
use Magne4000\EasyPz\Support\ProxyRequest;
use Magne4000\EasyPz\Support\ProxySignature;
use Psr\Http\Message\StreamInterface;
use Symfony\Component\HttpFoundation\Response;

/**
 * The public mod page and launcher endpoints of pzman, served under the
 * panel's URL: a streaming proxy with Range passthrough. Downloads are capped
 * across the panel since each one holds a PHP-FPM worker.
 */
class PublicProxyController extends Controller
{
    private const CHUNK = 65536;

    /** Slot lock lifetime; refreshed while the download runs, so a dead worker frees its slot within it. */
    private const SLOT_TTL = 120;

    private const SLOT_REFRESH = 30;

    public function __invoke(Request $request, string $uuidShort): Response
    {
        $server = Server::query()->where('uuid_short', $uuidShort)->first();
        $ez = $server && EasyPzService::eggHasFeature($server) ? EasyPzServer::enabledFor($server) : null;
        $path = ProxyRequest::pzmanPath($request->getPathInfo(), $uuidShort);
        if (!$ez || $path === null) {
            return $this->plain(404, 'Not found');
        }

        $lock = null;
        if (DownloadSlots::isDownload((string) $request->route('rest', ''))) {
            $slots = new DownloadSlots(fn (string $name) => Cache::lock($name, self::SLOT_TTL), (int) config('easy-pz.max_downloads', 5));
            $lock = $slots->acquire();
            if (!$lock) {
                return $this->plain(429, 'Too many downloads at once, retry in a few seconds.', ['Retry-After' => '5']);
            }
            // An aborted client ends the script at the next write, without
            // running finally blocks: shutdown functions still run.
            register_shutdown_function(fn () => $lock->release());
        }

        // The real method: the signature covers it, and Laravel's `_method`
        // override must not turn a POST into something else.
        $method = strtoupper($request->getRealMethod());
        if (!in_array($method, $request->route()->methods(), true)) {
            $lock?->release();

            return $this->plain(405, 'Method not allowed');
        }
        $headers = [
            ...ProxyRequest::forwardHeaders($method, $request->headers->all()),
            'X-Forwarded-For' => (string) $request->ip(),
            ProxySignature::HEADER => ProxySignature::header($ez->secretKey(), $method, $path),
        ];
        $query = $request->getQueryString();

        try {
            $upstream = (new Client())->request($method, EasyPzService::uiUrl($server, $ez) . $path . ($query ? "?$query" : ''), [
                'headers' => $headers,
                // POST files/<id>: the selection, streamed from php://input
                'body' => $method === 'POST' ? $request->getContent(true) : null,
                'stream' => true,
                'http_errors' => false,
                'allow_redirects' => false,
                'decode_content' => false,
                'connect_timeout' => 5,
                'read_timeout' => 60,
            ]);
        } catch (GuzzleException|\RuntimeException $exception) {
            $lock?->release();
            Log::warning('easy-pz: pzman unreachable from the public proxy', ['server' => $server->uuid, 'error' => $exception->getMessage()]);

            return $this->plain(502, 'This server\'s mod page is not reachable right now.');
        }

        $out = ProxyRequest::passBack($upstream->getHeaders()) + ['X-Accel-Buffering' => 'no'];
        $body = $upstream->getBody();

        return response()->stream(fn () => $this->pump($body, $lock), $upstream->getStatusCode(), $out);
    }

    private function pump(StreamInterface $body, ?Lock $lock): void
    {
        set_time_limit(0);
        while (ob_get_level() > 0) {
            ob_end_flush();
        }

        $refreshed = time();
        try {
            while (!$body->eof()) {
                $chunk = $body->read(self::CHUNK);
                if ($chunk === '') {
                    if ($body->getMetadata('timed_out')) {
                        break;
                    }

                    continue;
                }
                echo $chunk;
                flush();
                if (connection_aborted()) {
                    break;
                }
                if ($lock && time() - $refreshed >= self::SLOT_REFRESH) {
                    if (method_exists($lock, 'refresh')) {
                        $lock->refresh();
                    }
                    $refreshed = time();
                }
            }
        } finally {
            $body->close();
            $lock?->release();
        }
    }

    /** @param  array<string, string>  $headers */
    private function plain(int $status, string $message, array $headers = []): Response
    {
        return response($message, $status, $headers + [
            'Content-Type' => 'text/plain; charset=utf-8',
            'Cache-Control' => 'no-store',
            'X-Robots-Tag' => 'noindex, nofollow',
        ]);
    }
}
