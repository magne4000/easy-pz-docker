<?php

namespace Magne4000\EasyPz\Support;

use InvalidArgumentException;

/**
 * `.easy-pz/pzman.env`, per the "pzman.env" contract: one `KEY=value` per line,
 * values verbatim (no quotes, no expansion).
 */
final class PzmanEnv
{
    public const PATH = '.easy-pz/pzman.env';

    /**
     * The contract's keys for one server.
     *
     * @return array<string, string>
     */
    public static function values(int $panelPort, string $serverUuid, string $publicKeyHex, string $panelUrl, string $publicUrl, string $gameAddress): array
    {
        if (!SigningKey::isValidPublicKeyHex($publicKeyHex)) {
            throw new InvalidArgumentException('the public key must be 64 lowercase hex characters');
        }

        return [
            'PZMAN_HOST' => 'pelican',
            'PANEL_PORT' => (string) $panelPort,
            'PZMAN_PELICAN_SERVER' => $serverUuid,
            'PZMAN_PELICAN_PUBLIC_KEY' => $publicKeyHex,
            'PZMAN_PELICAN_PANEL_URL' => $panelUrl,
            'PANEL_PUBLIC_URL' => $publicUrl,
            'PANEL_PUBLIC_GAME_ADDRESS' => $gameAddress,
        ];
    }

    /**
     * @param  array<string, string>  $values
     */
    public static function render(array $values): string
    {
        $out = "# Written by the easy-pz Pelican plugin; edits are lost on the next Enable or Upgrade.\n";
        foreach ($values as $key => $value) {
            if (preg_match('/^[A-Z_][A-Z0-9_]*$/', (string) $key) !== 1) {
                throw new InvalidArgumentException("invalid pzman.env key: $key");
            }
            if (str_contains($value, "\n") || str_contains($value, "\r")) {
                throw new InvalidArgumentException("the value of $key contains a newline");
            }
            $out .= $key . '=' . $value . "\n";
        }

        return $out;
    }
}
