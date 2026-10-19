<?php

namespace Magne4000\EasyPz\Services;

use RuntimeException;
use Throwable;

class PzmanException extends RuntimeException
{
    /** @param  bool  $unreachable  no HTTP answer at all (connection refused, timeout, no UI URL) */
    public function __construct(string $message, int $status, public readonly bool $unreachable, ?Throwable $previous = null)
    {
        parent::__construct($message, $status, $previous);
    }
}
