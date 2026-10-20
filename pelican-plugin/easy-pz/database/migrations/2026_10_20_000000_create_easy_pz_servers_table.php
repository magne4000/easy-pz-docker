<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('easy_pz_servers', function (Blueprint $table) {
            $table->increments('id');
            $table->unsignedInteger('server_id')->unique();
            $table->foreign('server_id')->references('id')->on('servers')->cascadeOnDelete();
            // Ed25519 secret key, hex, encrypted with the app key; pzman gets the public key only.
            $table->text('signing_key');
            $table->unsignedInteger('ui_allocation_id')->nullable();
            $table->foreign('ui_allocation_id')->references('id')->on('allocations')->nullOnDelete();
            $table->string('ui_url_override')->nullable();
            $table->string('deployed_version')->nullable();
            $table->text('previous_startup')->nullable();
            $table->string('previous_auto_update')->nullable();
            $table->timestamp('enabled_at')->nullable();
            $table->timestamps();
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('easy_pz_servers');
    }
};
