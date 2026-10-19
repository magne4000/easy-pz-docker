<?php

namespace Magne4000\EasyPz\Services;

use App\Models\Server;
use App\Models\User;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Http\Client\Response;
use Illuminate\Support\Facades\Http;
use Magne4000\EasyPz\Models\EasyPzServer;
use RuntimeException;

/**
 * Service calls to pzman's API for the acting Pelican user: each request
 * carries a fresh single-use token with that user's scopes.
 */
class PzmanClient
{
    public function __construct(private readonly Server $server, private readonly EasyPzServer $ez, private readonly User $user) {}

    /**
     * @return array<string, mixed>
     *
     * @throws PzmanException
     */
    public function get(string $path): array
    {
        return $this->send('GET', $path);
    }

    /**
     * @param  array<string, mixed>|null  $body
     * @return array<string, mixed>
     *
     * @throws PzmanException
     */
    public function post(string $path, ?array $body = null): array
    {
        return $this->send('POST', $path, $body);
    }

    /**
     * @param  array<string, mixed>|null  $body
     * @return array<string, mixed>
     *
     * @throws PzmanException
     */
    private function send(string $method, string $path, ?array $body = null): array
    {
        try {
            $request = Http::baseUrl(EasyPzService::uiUrl($this->server, $this->ez))
                ->timeout(10)
                ->connectTimeout(3)
                ->acceptJson()
                ->withToken(EasyPzService::mintToken($this->user, $this->server, $this->ez));

            /** @var Response $response */
            $response = $body === null
                ? $request->send($method, '/api' . $path)
                : $request->asJson()->send($method, '/api' . $path, ['json' => $body]);
        } catch (ConnectionException $exception) {
            throw new PzmanException(trans('easy-pz::strings.errors.unreachable', ['error' => $exception->getMessage()]), 0, true, $exception);
        } catch (RuntimeException $exception) {
            throw new PzmanException($exception->getMessage(), 0, true, $exception);
        }

        if ($response->failed()) {
            $detail = $response->json('detail') ?? $response->json('title') ?? $response->reason();

            throw new PzmanException(trans('easy-pz::strings.errors.pzman', ['status' => $response->status(), 'detail' => $detail]), $response->status(), false);
        }

        return $response->status() === 204 ? [] : (array) $response->json();
    }
}
