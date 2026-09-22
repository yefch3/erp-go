-- +goose Up
-- Across versions, existing supplier commitments still cover the same product.
-- Keep each order unchanged and calculate remaining demand from the latest sale.
-- +goose StatementBegin
CREATE FUNCTION contract_requirement_group_open(p_tenant bigint,p_id bigint) RETURNS numeric
LANGUAGE plpgsql STABLE AS $$
DECLARE r purchase_requirements%ROWTYPE; latest integer; gross numeric; committed numeric;
BEGIN
 SELECT * INTO r FROM purchase_requirements WHERE tenant_id=p_tenant AND id=p_id;
 IF NOT FOUND THEN RETURN 0; END IF;
 IF r.contract_id=0 THEN RETURN greatest(r.required_qty-r.ordered_qty,0); END IF;
 SELECT max(version_no) INTO latest FROM purchase_requirements WHERE tenant_id=p_tenant AND contract_id=r.contract_id;
 IF r.version_no<>latest THEN RETURN 0; END IF;
 SELECT coalesce(sum(required_qty) FILTER(WHERE version_no=latest),0),coalesce(sum(ordered_qty),0)
 INTO gross,committed FROM purchase_requirements
 WHERE tenant_id=p_tenant AND contract_id=r.contract_id AND product_id=r.product_id
 AND coalesce(sku_id,0)=coalesce(r.sku_id,0) AND product_code=r.product_code
 AND product_name=r.product_name AND spec=r.spec AND uom_code=r.uom_code;
 RETURN greatest(gross-committed,0);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION contract_requirement_open(p_tenant bigint,p_id bigint) RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT greatest(least(required_qty-ordered_qty,contract_requirement_group_open(p_tenant,p_id)),0)
 FROM purchase_requirements WHERE tenant_id=p_tenant AND id=p_id
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION guard_contract_requirement_commitment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE available numeric;
BEGIN
 IF NEW.contract_id>0 AND NEW.ordered_qty>OLD.ordered_qty THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':contract-demand:'||NEW.contract_id::text,0));
  available:=contract_requirement_open(NEW.tenant_id,NEW.id);
  IF NEW.ordered_qty-OLD.ordered_qty>available THEN
   RAISE EXCEPTION 'Contract quantity already covered by purchase orders across versions' USING ERRCODE='23514',CONSTRAINT='contract_requirement_quantity_guard';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER contract_requirement_quantity_guard BEFORE UPDATE OF ordered_qty ON purchase_requirements
 FOR EACH ROW EXECUTE FUNCTION guard_contract_requirement_commitment();

-- +goose Down
DROP TRIGGER contract_requirement_quantity_guard ON purchase_requirements;
DROP FUNCTION guard_contract_requirement_commitment();
DROP FUNCTION contract_requirement_open(bigint,bigint);
DROP FUNCTION contract_requirement_group_open(bigint,bigint);
