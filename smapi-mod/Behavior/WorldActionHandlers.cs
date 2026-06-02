// Per-action handler files for world actions.
// Each is a minimal subclass of NpcActionHandlerBase. Override Execute
// when ready to implement real game logic.

using System;
using System.Text.Json;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    // ── World actions ────────────────────────────────────────────────

    internal sealed class WanderHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_wander";
        public WanderHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: pick random tiles near NPC and pathfind
    }

    internal sealed class ClearDebrisHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_clear_debris";
        public ClearDebrisHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby debris objects and remove them
    }

    internal sealed class WaterCropsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_water_crops";
        public WaterCropsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find unwatered crops in range and water them
    }

    internal sealed class HarvestCropsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_harvest_crops";
        public HarvestCropsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find harvestable crops and collect them
    }

    internal sealed class DepositItemsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_deposit_items";
        public DepositItemsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby chest and deposit held items
    }

    internal sealed class DeliverItemsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_deliver_items";
        public DeliverItemsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: pathfind to target and hand over items
    }

    internal sealed class ForageCollectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_forage_collect";
        public ForageCollectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find forage items in range and pick them up
    }

    internal sealed class PetAnimalHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_pet_animal";
        public PetAnimalHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby animal and pet it
    }

    internal sealed class PlantSeedsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_plant_seeds";
        public PlantSeedsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find tilled soil and plant seeds from inventory
    }

    internal sealed class TillSoilHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_till_soil";
        public TillSoilHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find diggable tiles and till them
    }

    internal sealed class InspectObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_inspect_object";
        public InspectObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: face target object and show inspection result
    }

    internal sealed class PlaceObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_place_object";
        public PlaceObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: place item from inventory onto target tile
    }
}
