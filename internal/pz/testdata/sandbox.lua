SandboxVars = {
    VERSION = 5,
    -- Changing this sets the "Population Multiplier" advanced option. Default=Normal
    -- 1 = Insane
    -- 2 = Very High
    -- 3 = High
    -- 4 = Normal
    -- 5 = Low
    Zombies = 4,
    -- How frequently homes and buildings will be discovered locked Default=Very Often
    -- 1 = Never
    -- 2 = Rare
    LockedHouses = 3,
    -- Days before water is shut off. Minimum=-1 Maximum=2147483647 Default=14
    WaterShutModifier = 14,
    -- A comma-separated list of item types that will be removed. 
    WorldItemRemovalList = "Base.Hat,Base.Glasses",
    -- When enabled certain melee weapons will be able to strike multiple zombies in one hit.
    MultiHitZombies = false,
    ZombieLore = {
        -- Controls the zombie movement rate. Default=Fast Shamblers
        -- 1 = Sprinters
        -- 2 = Fast Shamblers
        -- 3 = Shamblers
        Speed = 2,
    },
    ZombieConfig = {
        -- Set by the "Zombie Count" population option. Minimum=0.00 Maximum=4.00 Default=1.00
        PopulationMultiplier = 1.0,
    },
    SomeMod = {
        Nested = {
            Flag = true,
        },
    },
}
